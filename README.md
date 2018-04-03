# kube-toleration-admission


This is [Admission Webhook](https://kubernetes.io/docs/admin/extensible-admission-controllers/#admission-webhooks) that adds pod `tolerations` and optionally `nodeSelector`.

For every pod there will be set these tolerations:

```
tolerations:
- effect: NoExecute
  key: dedicated
  operator: Equal
  value: $(POD_NAMESPACE)
- effect: NoSchedule
  key: dedicated
  operator: Equal
  value: $(POD_NAMESPACE)
```


If the pod's namespace has annotation `smp.io/only-dedicated-nodes: "true"`, then `nodeSelector` also will be set:

```
nodeSelector:
  dedicated: $(POD_NAMESPACE)
```


## Installation

Create a deployment and a service:

Create MutatingAdmissionWebhook:


## Usage

1. Label and taint your nodes:

```
kubectl label node NODENAME dedicated=NAMESPACE
kubectl taint node NODENAME dedicated=NAMESPACE:NoExecute
```
