package main

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/influxdata/influxdb/logger"
	"k8s.io/apimachinery/pkg/util/runtime"

	"github.com/aws/aws-sdk-go/service/ec2"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// K8S_LABEL_AWS_REGION is key to retrieve the region from an Node running on AWS.
	K8S_LABEL_AWS_REGION = "failure-domain.beta.kubernetes.io/region"
	K8S_LABEL_PREFIX     = "awslabeler.com"
	// AWS_LABEL_PROFIX is the prefix that an AWS tag should have to be managed by the labeler.
	AWS_LABEL_PREFIX = "kubernetes/aws-labeler"
	// AWS_LABEL_K8S_LABEL is the prefix that an AWS tag should have to be set as label to kubernetes
	AWS_LABEL_K8S_LABEL = AWS_LABEL_PREFIX + "/label"
	// AWS_LABEL_K8S_TAINT is the prefix that an AWS tag should have to be set as taint to kubernetes
	AWS_LABEL_K8S_TAINT = AWS_LABEL_PREFIX + "/taint"
)

func main() {
	logger := logger.New(os.Stdout)
	defer logger.Sync() // flushes buffer, if any
	logger.Info("The k8s aws logger started")
	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		logger.Panic(err.Error())
		os.Exit(1)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logger.Panic(err.Error())
		os.Exit(1)
	}
	logger.Info("Kubernetes targeted", zap.String("host", config.Host), zap.String("username", config.Username))
	l := &labeler{
		clientSet: clientset,
		logger:    logger,
	}

	factory := informers.NewSharedInformerFactory(clientset, 0)
	informer := factory.Core().V1().Nodes().Informer()
	stopper := make(chan struct{})
	defer close(stopper)
	defer runtime.HandleCrash()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: l.onAdd,
	})
	go informer.Run(stopper)
	if !cache.WaitForCacheSync(stopper, informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}
	<-stopper
}

// isManagable checks if the EC2 Tag shoud be managed by the application.
func isManagable(i string) bool {
	return strings.Contains(i, AWS_LABEL_PREFIX)
}

// splitEC2Tag takes the EC2 tag and it checks if it has the right format
func splitEC2Tag(i string) ([]string, error) {
	res := strings.Split(i, "/")
	if len(res) != 4 {
		return nil, fmt.Errorf("wrong format for a tag.")
	}
	return res, nil
}

// getTaintFromAWSTag gets an EC2 tag name and tag value and it return a
// kubernetes taint
func getTaintFromAWSTag(res []string, tagValue string) (v1.Taint, error) {
	value := strings.Split(tagValue, ":")
	if len(value) != 2 {
		return v1.Taint{}, fmt.Errorf("wrong format for a tail's value. It shoud be like: labelValue:Effect")
	}
	var effect v1.TaintEffect
	switch value[1] {
	case "PreferNoSchedule":
		effect = v1.TaintEffectPreferNoSchedule
	case "NoExecute":
		effect = v1.TaintEffectNoExecute
	default:
		effect = v1.TaintEffectNoSchedule
	}
	return v1.Taint{
		Key:    K8S_LABEL_PREFIX + "/" + res[3],
		Value:  value[0],
		Effect: effect,
	}, nil
}

// onAdd is the function executed when the kubernetes informer notified the
// presence of a new kubernetes node in the cluster
func (l *labeler) onAdd(obj interface{}) {
	node := obj.(*corev1.Node)
	internalDNS := node.GetName()
	logger := l.logger.With(zap.String("node_name", internalDNS))
	instanceID, err := InstanceIDFromProviderID(node.Spec.ProviderID)
	if err != nil {
		logger.Error(err.Error())
		return
	}
	region, ok := node.GetLabels()[K8S_LABEL_AWS_REGION]
	if !ok {
		logger.Error("Region not found")
		return
	}
	s, err := session.NewSession()
	if err != nil {
		fmt.Print(err.Error())
		return
	}
	svc := ec2.New(s, aws.NewConfig().WithRegion(region))
	instance, err := getInstanceByID(svc, instanceID)
	if err != nil {
		logger.Error(err.Error())
		return
	}
	ll := node.GetLabels()
	taints := node.Spec.Taints
	cll := len(node.GetLabels())
	cTaints := len(taints)

	for _, t := range instance.Tags {
		logger := logger.With(zap.String("tag:"+*t.Key, *t.Value))
		if !isManagable(*t.Key) {
			continue
		}
		res, err := splitEC2Tag(*t.Key)
		if err != nil {
			logger.Error(err.Error())
			continue
		}
		if strings.Contains(*t.Key, AWS_LABEL_K8S_LABEL) {
			ll[K8S_LABEL_PREFIX+"/"+res[3]] = *t.Value
		}
		if strings.Contains(*t.Key, AWS_LABEL_K8S_TAINT) {
			taint, err := getTaintFromAWSTag(res, *t.Value)
			if err != nil {
				logger.Error(err.Error())
				continue
			}
			taints = append(taints, taint)
		}
	}

	if cll != len(ll) || cTaints != len(taints) {
		// TODO: Implement this as PATCH and not with this dirty awesome hack.
		n, err := l.clientSet.CoreV1().Nodes().Get(internalDNS, metav1.GetOptions{})
		if err != nil {
			logger.Error(err.Error())
			return
		}
		n.Spec.Taints = taints
		n.SetLabels(ll)
		_, err = l.clientSet.CoreV1().Nodes().Update(n)
		if err != nil {
			logger.Error(err.Error())
			return
		}
		logger.Info("Added new kubernetes labels.")
	}
}

type labeler struct {
	clientSet *kubernetes.Clientset
	logger    *zap.Logger
}

// InstanceIDFromProviderID parses the ProviderID and it returns an AWS instance ID
func InstanceIDFromProviderID(i string) (string, error) {
	s := strings.Split(i, "/")
	if len(s) != 5 {
		return "", fmt.Errorf("Expected 5 parts after a split")
	}
	return s[4], nil
}

// getInstanceByID retrieves the ec2 instance from AWS
func getInstanceByID(svc *ec2.EC2, instanceID string) (*ec2.Instance, error) {
	instancesOutput, err := svc.DescribeInstances(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{&instanceID},
	})
	if err != nil {
		return nil, err
	}
	if len(instancesOutput.Reservations) != 1 {
		return nil, fmt.Errorf("expected one reservation")
	}
	if len(instancesOutput.Reservations[0].Instances) != 1 {
		return nil, fmt.Errorf("expected one instance")
	}
	return instancesOutput.Reservations[0].Instances[0], nil
}
