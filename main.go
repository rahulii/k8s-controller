package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)


func main() {
	kubeconfig:= flag.String("kubeconfig","/home/infracloud/.kube/config","kubeconfig file")
	flag.Parse()

	config,err := clientcmd.BuildConfigFromFlags("",*kubeconfig)

	if err != nil {
		log.Fatalf("Error parsing kubeconfig file %v",err)
	}

	clinetset,err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error clientset %v",err)
	}
	ch := make(chan struct{})

	informerFactory := informers.NewSharedInformerFactory(clinetset,10 * time.Minute)

	c := newController(*clinetset,informerFactory.Apps().V1().Deployments())
	informerFactory.Start(ch)
	c.run(ch)
	fmt.Println(*clinetset)

}