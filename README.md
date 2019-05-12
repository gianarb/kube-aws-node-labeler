I was looking for a way to automatically set labels to a Kubernetes node based
on the tags the EC2 has.

I wrote a small application that uses an informer to take action every time a
new node joins the cluster. If the node has the label
`failure-domain.beta.kubernetes.io/region` it is able to look for the instance
tags from AWS. Specifically it looks for instance tags like:

```
kubernetes/aws-labeler/label/type=ci
```
If the EC2 has this tag the Kubernetes node will have a label called

```
awslabeler.com/type=ci
```
That you can use to schedule your pods.

Other than labels it also support taints:

AWS EC2 tags like:
```
kubernetes/aws-labeler/taint/type=ci:NoExecute
```
Will become:

```
awslabeler.com/type=ci:NoExecute
```

## Build

```
$ make docker
```
Creates an image


## Deploy to Kubernetes

1. You need to create a service account for this `pod` becauase it needs access
   to the nodes resources. Get, watch, update is enough.
2. You need to give access to AWS so you need to configure a IAM role for your
   pod, or share a credential file or via envvar.

## Try locally

It really depends on your setup as usual but in my case I use this command:
```
AWS_PROFILE=my-credentials-profile KUBECONFIG=/home/myhome/.kube/config go run cmd/aws-node-labeler/main.go
```
