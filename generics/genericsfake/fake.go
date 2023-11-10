package main

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/watch"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
)

type Clientset struct {
	testing.Fake
	tracker testing.ObjectTracker
}

// NewSimpleClientset returns a clientset that will respond with the provided objects.
// It's backed by a very simple object tracker that processes creates, updates and deletions as-is,
// without applying any validations and/or defaults. It shouldn't be considered a replacement
// for a real clientset and is mostly useful in simple unit tests.
func NewSimpleClientset(objects ...runtime.Object) *Clientset {

	s := runtime.NewScheme()
	metav1.AddMetaToScheme(s)
	corev1.AddToScheme(s)
	cf := serializer.NewCodecFactory(s)
	o := testing.NewObjectTracker(s, cf.UniversalDecoder())
	for _, obj := range objects {
		if err := o.Add(obj); err != nil {
			panic(err)
		}
	}

	cs := &Clientset{tracker: o}
	cs.AddReactor("*", "*", testing.ObjectReaction(o))
	cs.AddWatchReactor("*", func(action testing.Action) (handled bool, ret watch.Interface, err error) {
		gvr := action.GetResource()
		ns := action.GetNamespace()
		watch, err := o.Watch(gvr, ns)
		if err != nil {
			return false, nil, err
		}
		return true, watch, nil
	})

	return cs
}

type fakeAPI[T Resource] struct {
	*testing.Fake
	tracker testing.ObjectTracker
}

func NewFake[T Resource](objects ...T) FakeAPI[T] {
	csf := NewSimpleClientset()
	f := &csf.Fake
	o := csf.tracker
	for _, obj := range objects {
		if err := o.Add(any(&obj).(runtime.Object)); err != nil {
			panic(err)
		}
	}

	cs := &fakeAPI[T]{
		tracker: o,
		Fake:    f,
	}
	//cs.AddReactor("*", "*", testing.ObjectReaction(o))
	//cs.AddWatchReactor("*", func(action testing.Action) (handled bool, ret watch.Interface, err error) {
	//	gvr := action.GetResource()
	//	ns := action.GetNamespace()
	//	watch, err := o.Watch(gvr, ns)
	//	if err != nil {
	//		return false, nil, err
	//	}
	//	return true, watch, nil
	//})

	return cs
}

func (f fakeAPI[T]) Get(name string, namespace string, options metav1.GetOptions) (*T, error) {
	x := new(T)
	gvr := (*x).ResourceMetadata().WithResource((*x).ResourceName())
	obj, err := f.Fake.
		Invokes(testing.NewGetAction(gvr, namespace, name), any(x).(runtime.Object))

	if obj == nil {
		return nil, err
	}
	return any(obj).(*T), err
}

func typeName(o any) string {
	t := reflect.TypeOf(o)
	if t.Kind() != reflect.Ptr {
		panic("All types must be pointers to structs.")
	}
	t = t.Elem()
	return t.Name()
}

func (f fakeAPI[T]) List(namespace string, options metav1.ListOptions) ([]T, error) {
	x := new(T)
	// I guess we should make ResourceMetadata have resource!
	gvr := (*x).ResourceMetadata().WithResource((*x).ResourceName())
	gvk := (*x).ResourceMetadata().WithKind(typeName(x))
	obj, err := f.Fake.
		Invokes(testing.NewListAction(gvr, gvk, namespace, options), &GenericList[T]{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(options)
	if label == nil {
		label = labels.Everything()
	}
	return reflect.ValueOf(obj).Elem().FieldByName("Items").Interface().([]T), nil
}

func (f fakeAPI[T]) Watch(namespace string, options metav1.ListOptions) (Watcher[T], error) {
	x := new(T)
	gvr := (*x).ResourceMetadata().WithResource((*x).ResourceName())
	wi, err := f.Fake.
		InvokesWatch(testing.NewWatchAction(gvr, namespace, options))
	if err != nil {
		return Watcher[T]{}, err
	}
	return newWatcher[T](wi), nil
}

func (f fakeAPI[T]) Create(t T, options metav1.CreateOptions) (*T, error) {
	x := new(T)
	gvr := (*x).ResourceMetadata().WithResource((*x).ResourceName())
	meta := (any)(&t).(metav1.Object)
	obj, err := f.Fake.
		Invokes(testing.NewCreateAction(gvr, meta.GetNamespace(), any(&t).(runtime.Object)), any(x).(runtime.Object))
	if err != nil {
		return nil, err
	}
	return any(obj).(*T), nil
}

func (f fakeAPI[T]) Update(t T, options metav1.UpdateOptions) (*T, error) {
	x := new(T)
	gvr := (*x).ResourceMetadata().WithResource((*x).ResourceName())
	meta := (any)(&t).(metav1.Object)
	obj, err := f.Fake.
		Invokes(testing.NewUpdateAction(gvr, meta.GetNamespace(), any(&t).(runtime.Object)), any(x).(runtime.Object))
	if err != nil {
		return nil, err
	}
	return any(obj).(*T), nil
}

func (f fakeAPI[T]) Namespace(namespace string) NamespacedAPI[T] {
	// TODO implement me
	panic("implement me")
}

var _ FakeAPI[Resource] = fakeAPI[Resource]{}

type FakeAPI[T Resource] interface {
	API[T]
}
