package main

import (
	"log"
	"path/filepath"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/generics"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func main() {
	home := homedir.HomeDir()
	kubeconfig := filepath.Join(home, ".kube", "config")
	// use the current context in kubeconfig
	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatal(err)
	}
	s := runtime.NewScheme()
	metav1.AddMetaToScheme(s)
	rbacv1.AddToScheme(s)
	cf := serializer.NewCodecFactory(s)
	restConfig.NegotiatedSerializer = cf.WithoutConversion()
	restConfig.GroupVersion = &schema.GroupVersion{Version: "v1", Group: "rbac.authorization.k8s.io"}
	rc, err := rest.RESTClientFor(restConfig)
	if err != nil {
		log.Fatal(err)
	}
	c := generics.NewClient(rc, s)
	for _, dep := range generics.MustList[rbacv1.Role](c, "kube-system") {
		log.Println(dep.Name)
	}
}
