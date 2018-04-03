package main

import (
    "log"
    "flag"
    "net/http"
    "io/ioutil"
    "crypto/tls"
    "encoding/json"

    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/serializer"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
    "k8s.io/api/admission/v1beta1"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
)

// TODO: smp.io/dedicated (add option for this)
const taintName string = "dedicated"
const labelName string = "dedicated"
const nsAnnotation string = "smp.io/force-dedicated-nodes"

type operation struct {
    Op     string      `json:"op"`
    Path   string      `json:"path"`
    Value  interface{} `json:"value"`
}

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)

func init() {
    corev1.AddToScheme(scheme)
    admissionregistrationv1beta1.AddToScheme(scheme)
}

func main() {
    var CertFile string
    var KeyFile  string

    flag.StringVar(&CertFile, "tls-cert-file", CertFile, ""+
        "File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
        "after server cert).")
    flag.StringVar(&KeyFile, "tls-key-file", KeyFile, ""+
        "File containing the default x509 private key matching --tls-cert-file.")
    flag.Parse()

    http.HandleFunc("/", mkServe(getClient()))
    server := &http.Server{
        Addr:      ":443",
        TLSConfig: configTLS(CertFile, KeyFile),
    }
    server.ListenAndServeTLS("", "")

}

func configTLS(CertFile string, KeyFile  string) *tls.Config {
    sCert, err := tls.LoadX509KeyPair(CertFile, KeyFile)
    if err != nil {
        log.Fatal(err)
    }
    return &tls.Config{
        Certificates: []tls.Certificate{sCert},
    }
}

func mkServe(clientset *kubernetes.Clientset) func(http.ResponseWriter, *http.Request) {
    return func(w http.ResponseWriter, r *http.Request) {
        var body []byte
        if r.Body != nil {
            if data, err := ioutil.ReadAll(r.Body); err == nil {
                body = data
            }
        }
        log.Print(string(body))

        // verify the content type is accurate
        contentType := r.Header.Get("Content-Type")
        if contentType != "application/json" {
            log.Printf("contentType=%s, expect application/json", contentType)
            return
        }

        var reviewResponse *v1beta1.AdmissionResponse
        ar := v1beta1.AdmissionReview{}
        deserializer := codecs.UniversalDeserializer()
        if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
            log.Print(err)
            reviewResponse = toAdmissionResponse(err)
        } else {
            reviewResponse = admit(ar, clientset)
        }

        response := v1beta1.AdmissionReview{}
        if reviewResponse != nil {
            response.Response = reviewResponse
            response.Response.UID = ar.Request.UID
        }
        // reset the Object and OldObject, they are not needed in a response.
        ar.Request.Object = runtime.RawExtension{}
        ar.Request.OldObject = runtime.RawExtension{}

        resp, err := json.Marshal(response)
        if err != nil {
            log.Print(err)
        }
        if _, err := w.Write(resp); err != nil {
            log.Print(err)
        }
    }
}

func toAdmissionResponse(err error) *v1beta1.AdmissionResponse {
    return &v1beta1.AdmissionResponse{
        Result: &metav1.Status{
            Message: err.Error(),
        },
    }
}

func admit(ar v1beta1.AdmissionReview, clientset *kubernetes.Clientset) *v1beta1.AdmissionResponse {
    podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
    if ar.Request.Resource != podResource {
        log.Printf("expected resource to be %s", podResource)
        return nil
    }

    if ar.Request.Operation != "CREATE" {
        log.Printf("expected operation to be %s", "CREATE")
        return nil
    }

    raw := ar.Request.Object.Raw
    pod := corev1.Pod{}
    deserializer := codecs.UniversalDeserializer()
    if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
        log.Print(err)
        return toAdmissionResponse(err)
    }

    reviewResponse := v1beta1.AdmissionResponse{}
    reviewResponse.Allowed = true

    if label, ok := pod.Labels["run"]; ok {
        if label == "to-be-mutated" {
            operations := makePatch(&pod, ar.Request.Namespace, clientset)

            if len(operations) != 0 {
                patch, err := json.Marshal(operations)
                if err != nil {
                    log.Print(err)
                    return toAdmissionResponse(err)
                }

                log.Print(string(patch))
                reviewResponse.Patch = patch
                pt := v1beta1.PatchTypeJSONPatch
                reviewResponse.PatchType = &pt
            }
        }
    }

    return &reviewResponse
}

func makePatch(pod *corev1.Pod, namespace string, clientset *kubernetes.Clientset) []*operation {
    ops := []*operation{}

    if !hasTolerationEffect(pod, corev1.TaintEffectNoExecute) {
        ops = append(ops, makeTolerationOperation(corev1.TaintEffectNoExecute, namespace))
    }
    if !hasTolerationEffect(pod, corev1.TaintEffectNoSchedule) {
        ops = append(ops, makeTolerationOperation(corev1.TaintEffectNoSchedule, namespace))
    }

    _, ok := pod.Spec.NodeSelector[labelName]
    if ok {
        return ops
    }

    ns, err := clientset.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
    if err != nil {
        log.Print(err)
        return ops
    }

    log.Print(ns.Annotations)
    annotation, ok := ns.Annotations[nsAnnotation]
    if !ok || annotation != "true" {
        return ops
    }

    if len(pod.Spec.NodeSelector) == 0 {
        ops = append(ops, &operation{
            Op: "add",
            Path: "/spec/nodeSelector",
            Value: map[string]string{labelName: namespace},
        })
    } else {
        ops = append(ops, &operation{
            Op: "add",
            Path: "/spec/nodeSelector/" + labelName,
            Value: namespace,
        })
    }

    return ops
}

func hasTolerationEffect(pod *corev1.Pod, effect corev1.TaintEffect) bool {
    for _, toleration := range pod.Spec.Tolerations {
        if toleration.Effect == effect && toleration.Key == taintName {
            return true
        }
    }

    return false
}

func makeTolerationOperation(effect corev1.TaintEffect, namespace string) *operation {
    return &operation{
        Op: "add",
        Path: "/spec/tolerations/0",
        Value: &corev1.Toleration{
            Effect: effect,
            Key: taintName,
            Operator: "Equal",
            Value: namespace,
        },
    }
}

func getClient() *kubernetes.Clientset {
    config, err := rest.InClusterConfig()
    if err != nil {
        log.Fatal(err)
    }
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        log.Fatal(err)
    }
    return clientset
}
