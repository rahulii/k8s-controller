package main

import (
	"context"
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type controller struct{
	clientset kubernetes.Clientset
	depLister appslisters.DeploymentLister
	depCacheSynced cache.InformerSynced
	queue workqueue.RateLimitingInterface
}

func newController(clientset kubernetes.Clientset, depInformer appsinformers.DeploymentInformer) *controller {
	c := &controller{
		clientset: clientset,
		depLister: depInformer.Lister(),
		depCacheSynced: depInformer.Informer().HasSynced,
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(),"ekspose"),
	}

	depInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: c.handleAdd,
			DeleteFunc: c.handleDelete,
		},
	)



	return c
}

func (c *controller) run(ch <- chan struct{} ){
	fmt.Println("Starting controler")
	if !cache.WaitForCacheSync(ch,c.depCacheSynced){
		fmt.Println("Error cache sync")
	}

	go wait.Until(c.worker,1*time.Second,ch)

	<-ch

}

func (c *controller) worker() {
	for c.processItem(){

	}
}

func (c *controller) processItem() bool {
	item,shutdown := c.queue.Get()
	if shutdown{
		return false
	}

	defer c.queue.Forget(item)

	key,err := cache.MetaNamespaceKeyFunc(item)
	if err != nil {
		log.Fatal("Error getting key from cache %v",err)
		return false
	}

	ns,name,err := cache.SplitMetaNamespaceKey(key)
	fmt.Println(ns + " "+name)
	if err != nil {
		log.Fatal("Error splitting key into ns and name %v ",err)
		return false
	}

	//check if the object has been deleted from k8s server

	ctx := context.Background()
	dep,err := c.clientset.AppsV1().Deployments(ns).Get(ctx,name,metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		fmt.Println("handle delete event for",dep.Name)

		//delete servive
		err := c.clientset.CoreV1().Services(ns).Delete(ctx,name,metav1.DeleteOptions{})
		if err != nil {
			fmt.Printf("error:delete service %s",err.Error())
			return false
		}
		return true

	}
	err  = c.syncDeployment(ns,name)
	if err != nil {

		//re-try
		fmt.Println("error syncing deployment %v",err)
		return false
	}
	return true

}

func (c *controller) syncDeployment(ns,name string) error{

	dep,err1 := c.depLister.Deployments(ns).Get(name)
	if err1 != nil {
		return err1
	}
	//create service
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: dep.Name,
			Namespace: ns,
		},
		Spec: corev1.ServiceSpec{
			Selector: dep.Labels,
			Ports: []corev1.ServicePort{
				corev1.ServicePort{
					Name: "http",
					Port: 80,
				},
			},
		},
	}
	ctx := context.Background()
	s,err := c.clientset.CoreV1().Services(ns).Create(ctx,&svc,metav1.CreateOptions{})
	if err != nil {
		return err
	}
	//create ingress
	return createIngress(ctx, s, c.clientset)

}

func createIngress( ctx context.Context ,  svc *corev1.Service , clientset kubernetes.Clientset) error {
	pathType := "Prefix"
	ingress := netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: svc.Name,
			Namespace: svc.Namespace,
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/rewrite-target": "/",
			},
		},
		Spec: netv1.IngressSpec{
			Rules: []netv1.IngressRule{
				{
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path: fmt.Sprintf("/%s",svc.Name),
									PathType: (*netv1.PathType)(&pathType),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: svc.Name,
											Port: netv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},

								},
							},
						},
					},
				},
			},
		},
	}

	_,err :=clientset.NetworkingV1().Ingresses(svc.Namespace).Create(ctx,&ingress,metav1.CreateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (c *controller) handleAdd(obj interface{}) {
	fmt.Println("Add was called")
	c.queue.Add(obj)
}

func (c *controller) handleDelete(obj interface{}) {
	fmt.Println("Delete was called")
	c.queue.Add(obj)
}