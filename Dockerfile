FROM scratch
COPY kube-aws-node-labeler /
ENTRYPOINT ["/kube-aws-node-labeler"]
