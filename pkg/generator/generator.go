package generator

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/uzxmx/k8s-event-generator/utils"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/reference"
	"k8s.io/klog/v2"
	"math/rand"
	"time"
)

// Generator generates event.
type Generator struct {
	kind      string
	name      string
	namespace string
	selector  map[string]string

	eventType    string
	eventAction  string
	eventReason  string
	eventMessage string

	clientset        *kubernetes.Clientset
	restClientGetter resource.RESTClientGetter
}

const (
	defaultNamespace = "spot-lab"
	defaultEventType = v1.EventTypeWarning
	defaultLabel     = "spot=canbereleased"
)

var defaultLabelSelector = map[string]string{"spot": "canbereleased"}

// NewGenerator creates a generator.
func NewGenerator() *Generator {
	clientset, err := utils.GetKubeClient("", "")
	if err != nil {
		klog.Fatalf("unable get client:%v", err)
	}
	return &Generator{
		clientset: clientset,
	}
}

// AddFlags adds flags to command.
func (g *Generator) AddFlags(cmd *cobra.Command) {
	configFlags := genericclioptions.NewConfigFlags(true)
	configFlags.AddFlags(cmd.PersistentFlags())
	g.restClientGetter = configFlags

	flags := cmd.Flags()
	flags.StringVar(&g.kind, "kind", "pods", "Resource kind to get.")
	//flags.StringVar(&g.name, "name", "spotdeployment-6f6c6b455c-podreplaced-2", "Resource name to get.")
	flags.StringVar(&g.namespace, "namespace", defaultNamespace, "Resource namespace to get.")
	flags.StringVar(&g.eventType, "type", defaultEventType, "Event type.")
	flags.StringVar(&g.eventAction, "action", "", "Event action.")
	flags.StringVar(&g.eventReason, "reason", "SpotToBeReleased", "Event reason.")
	flags.StringVar(&g.eventMessage, "message", "Spot ECI will be released in 3 minutes", "Event message.")
}

// Run generates event.
func (g *Generator) Run() error {
	rand.Seed(time.Now().Unix())
	for {
		<-time.After(60 * time.Second)
		HasChosePod, err := g.ChosePod()
		if err != nil {
			klog.ErrorS(err, "failed to chose pod")
			continue
		}
		if HasChosePod {
			g.SendOneEvent()
		}
	}
	return nil
}

func (g *Generator) ChosePod() (bool, error) {
	pods, err := g.clientset.CoreV1().Pods(g.namespace).List(metav1.ListOptions{LabelSelector: defaultLabel})
	if err != nil {
		klog.ErrorS(err, "Unable list pods")
		return false, err
	}

	if len(pods.Items) == 0 {
		klog.InfoS("there is no spotpod can be replaced")
		return false, nil
	}
	for _, pod := range pods.Items {
		klog.InfoS("list pod %v", pod.Name)
	}
	chosedpod := pods.Items[rand.Intn(len(pods.Items))]
	g.name = chosedpod.Name

	return true, nil
}

func (g *Generator) SendOneEvent() error {
	r := resource.NewBuilder(g.restClientGetter).
		Unstructured().
		NamespaceParam(g.namespace).
		ResourceTypeOrNameArgs(true, g.kind, g.name).
		Do()
	if err := r.Err(); err != nil {
		return err
	}

	infos, err := r.Infos()
	if err != nil {
		return err
	}

	ref, err := reference.GetReference(scheme.Scheme, infos[0].Object)
	if err != nil {
		return err
	}

	restConfig, err := g.restClientGetter.ToRESTConfig()
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	if len(g.eventAction) == 0 {
		g.eventAction = g.eventReason
	}

	now := time.Now()
	event, err := client.CoreV1().Events("").CreateWithEventNamespace(&v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("fakeclient.%v.%x", g.name, now.UnixNano()),
			Namespace: g.namespace,
		},
		FirstTimestamp:      metav1.NewTime(now),
		LastTimestamp:       metav1.NewTime(now),
		EventTime:           metav1.NewMicroTime(now),
		ReportingController: "k8s-event-generator",
		ReportingInstance:   "k8s-event-generator",
		Action:              g.eventAction,
		InvolvedObject:      *ref,
		Reason:              g.eventReason,
		Type:                g.eventType,
		Message:             g.eventMessage,
	})

	if err == nil {
		klog.Infof("Event generated successfully: %v, %v", event.Name, event.InvolvedObject)
	}

	return err
}
