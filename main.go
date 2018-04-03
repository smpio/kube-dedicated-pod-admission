package main

import (
    "flag"
    "strings"
    "net/http"
    "io/ioutil"
    "crypto/tls"
    "encoding/json"

    "github.com/golang/glog"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/serializer"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
    "k8s.io/api/admission/v1beta1"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
)

const taintName string = "dedicated"
const labelName string = "dedicated"

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
        glog.Fatal(err)
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

        // verify the content type is accurate
        contentType := r.Header.Get("Content-Type")
        if contentType != "application/json" {
            glog.Errorf("contentType=%s, expect application/json", contentType)
            return
        }

        var reviewResponse *v1beta1.AdmissionResponse
        ar := v1beta1.AdmissionReview{}
        deserializer := codecs.UniversalDeserializer()
        if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
            glog.Error(err)
            reviewResponse = toAdmissionResponse(err)
        } else {
            reviewResponse = admit(ar)
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
            glog.Error(err)
        }
        if _, err := w.Write(resp); err != nil {
            glog.Error(err)
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

func admit(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
    glog.Info("---1")
    podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
    if ar.Request.Resource != podResource {
        glog.Errorf("expect resource to be %s", podResource)
        return nil
    }

    glog.Info("---2")
    raw := ar.Request.Object.Raw
    pod := corev1.Pod{}
    deserializer := codecs.UniversalDeserializer()
    if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
        glog.Error(err)
        return toAdmissionResponse(err)
    }

    glog.Info("---3")
    reviewResponse := v1beta1.AdmissionResponse{}
    reviewResponse.Allowed = true
    if strings.HasPrefix(pod.Name, "to-be-mutated") {
        glog.Info("---4")
        patch, err := json.Marshal(makePatch(&pod))
        if err != nil {
            glog.Error(err)
            return toAdmissionResponse(err)
        }
        glog.Info("---5")
        glog.Info(string(patch))
        reviewResponse.Patch = patch
        pt := v1beta1.PatchTypeJSONPatch
        reviewResponse.PatchType = &pt
    }
    return &reviewResponse
}

func makePatch(pod *corev1.Pod) []*operation {
    ops := []*operation{}
    if !hasTolerationEffect(pod, corev1.TaintEffectNoExecute) {
        ops = append(ops, makeOperation(corev1.TaintEffectNoExecute, pod.Namespace))
    }
    if !hasTolerationEffect(pod, corev1.TaintEffectNoSchedule) {
        ops = append(ops, makeOperation(corev1.TaintEffectNoSchedule, pod.Namespace))
    }

    // _, err = clientset.CoreV1().Pods("default").Get("example-xxxxx", metav1.GetOptions{})

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

func makeOperation(effect corev1.TaintEffect, namespace string) *operation {
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
        glog.Fatal(err)
    }
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        glog.Fatal(err)
    }
    return clientset
}
