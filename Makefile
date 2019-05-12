FROM docker.io/golang:1.12-stretch
COPY . /go/src/github.com/influxdata/idpe
WORKDIR /go/src/github.com/influxdata/idpe

RUN GO111MODULE=on GOOS=linux CGO_ENABLED=0 go build -mod=vendor -o /awslabeler ./cmd/k8s-aws-labeler

FROM scratch
COPY --from=0 /awslabeler /awslabeler
COPY --from=0 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
CMD ["/awslabeler"]
