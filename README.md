# kube-dedicated-pod-admission


This is [Admission Webhook](https://kubernetes.io/docs/admin/extensible-admission-controllers/#admission-webhooks) that adds pod `tolerations` and optionally `nodeSelector`.

For every pod there will be set these tolerations:

```
tolerations:
- effect: NoExecute
  key: smp.io/dedicated
  operator: Equal
  value: $(POD_NAMESPACE)
- effect: NoSchedule
  key: smp.io/dedicated
  operator: Equal
  value: $(POD_NAMESPACE)
```


If the pod's namespace has annotation `smp.io/only-dedicated-nodes: "true"`, then `nodeSelector` also will be set:

```
nodeSelector:
  smp.io/dedicated: $(POD_NAMESPACE)
```


## Installation

See [Kubernetes docs](https://kubernetes.io/docs/admin/extensible-admission-controllers/#admission-webhooks).


## Usage

1. Label and taint your nodes:

```
kubectl label node NODENAME smp.io/dedicated=NAMESPACE
kubectl taint node NODENAME smp.io/dedicated=NAMESPACE:NoExecute
```

2. (Optinally) annotate namespaces:

```
kubectl annotate ns NAMESPACE smp.io/only-dedicated-nodes=true
```
