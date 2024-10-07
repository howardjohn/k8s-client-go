package generics

import (
	"context"
	"reflect"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type ObjectList[T any] struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []T `json:"items"`
}

func (o ObjectList[T]) GetObjectKind() schema.ObjectKind {
	return o.TypeMeta.GetObjectKind()
}

func (o ObjectList[T]) DeepCopyObject() runtime.Object {
	n := ObjectList[T]{
		TypeMeta: o.TypeMeta,
		ListMeta: o.ListMeta,
	}
	for _, i := range o.Items {
		x := (any)(i).(runtime.Object)
		n.Items = append(n.Items, x.DeepCopyObject().(T))
	}
	return n
}

var _ runtime.Object = ObjectList[any]{}

type API[T Resource] interface {
	Create(t T, options metav1.CreateOptions) (*T, error)
	Update(t T, options metav1.UpdateOptions) (*T, error)
	Get(name string, namespace string, options metav1.GetOptions) (*T, error)
	List(namespace string, options metav1.ListOptions) ([]T, error)
	Watch(namespace string, options metav1.ListOptions) (Watcher[T], error)
}

type api[T Resource, PT interface {
	runtime.Object
	*T
}] struct {
	c *Client
}

func (a api[T, PT]) Get(name, namespace string, options metav1.GetOptions) (*T, error) {
	return Get[T, PT](a.c, name, namespace, options)
}

func (a api[T, PT]) List(namespace string, options metav1.ListOptions) ([]T, error) {
	return List[T](a.c, namespace, options)
}

func (a api[T, PT]) Watch(namespace string, options metav1.ListOptions) (Watcher[T], error) {
	return Watch[T](a.c, namespace, options)
}

func (a api[T, PT]) Create(t T, options metav1.CreateOptions) (*T, error) {
	return Create[T](a.c, t, options)
}

func (a api[T, PT]) Update(t T, options metav1.UpdateOptions) (*T, error) {
	return Update[T](a.c, t, options)
}

//var _ API[Resource, runtime.Object] = api[Resource, runtime.Object]{}

func NewAPI[T Resource, PT interface {
	runtime.Object
	*T
}](c *Client) API[T] {
	return api[T, PT]{c: c}
}

type Resource interface {
}

func resourceAsobject(r Resource) runtime.Object {
	return any(r).(runtime.Object)
}

type Client struct {
	client         rest.Interface
	parameterCodec runtime.ParameterCodec
	scheme         *runtime.Scheme
}

func NewClient(rc rest.Interface, s *runtime.Scheme) *Client {
	return &Client{
		client:         rc,
		scheme:         s,
		parameterCodec: runtime.NewParameterCodec(s),
	}
}

func Get[T Resource, PT interface {
	runtime.Object
	*T
}](c *Client, name, namespace string, options metav1.GetOptions) (*T, error) {
	result := new(T)
	gv := resourceMetadata[T](c)
	x := (any)(result).(runtime.Object)
	err := c.client.Get().
		Namespace(namespace).
		Resource(gv.Resource).
		Name(name).
		VersionedParams(&options, c.parameterCodec).
		AbsPath(defaultPath(gv)).
		Do(context.Background()).
		Into(x)
	return result, err
}

func resourceMetadata[T Resource](c *Client) schema.GroupVersionResource {
	n := any(new(T))
	kinds, _, _ := c.scheme.ObjectKinds(n.(runtime.Object))
	k := kinds[0]
	return schema.GroupVersionResource{
		Group:    k.Group,
		Version:  k.Version,
		Resource: Name[T](),
	}
}

func Create[T Resource](c *Client, t T, options metav1.CreateOptions) (*T, error) {
	gv := resourceMetadata[T](c)
	result := new(T)
	x := (any)(result).(runtime.Object)
	meta := (any)(t).(metav1.Object)

	err := c.client.Post().
		Namespace(meta.GetNamespace()).
		Resource(gv.Resource).
		Name(meta.GetName()).
		VersionedParams(&options, c.parameterCodec).
		AbsPath(defaultPath(gv)).
		Do(context.Background()).
		Into(x)
	return result, err
}

func Update[T Resource](c *Client, t T, options metav1.UpdateOptions) (*T, error) {
	result := new(T)
	gv := resourceMetadata[T](c)
	x := (any)(result).(runtime.Object)
	meta := (any)(t).(metav1.Object)

	err := c.client.Put().
		Namespace(meta.GetNamespace()).
		Resource(gv.Resource).
		Name(meta.GetName()).
		VersionedParams(&options, c.parameterCodec).
		AbsPath(defaultPath(gv)).
		Do(context.Background()).
		Into(x)
	return result, err
}

func List[T Resource](c *Client, namespace string, opts metav1.ListOptions) ([]T, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	x := ObjectList[T]{}
	gv := resourceMetadata[T](c)
	err := c.client.Get().
		Namespace(namespace).
		Resource(gv.Resource).
		Timeout(timeout).
		VersionedParams(&opts, c.parameterCodec).
		AbsPath(defaultPath(gv)).
		Do(context.Background()).
		Into(&x)
	return x.Items, err
}

func MustList[T Resource](c *Client, namespace string) []T {
	res, err := List[T](c, namespace, metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}
	return res
}

func Watch[T Resource](c *Client, namespace string, options metav1.ListOptions) (Watcher[T], error) {
	gv := resourceMetadata[T](c)
	options.Watch = true
	wi, err := c.client.Get().
		Namespace(namespace).
		Resource(gv.Resource).
		VersionedParams(&options, c.parameterCodec).
		AbsPath(defaultPath(gv)).
		Watch(context.Background())
	if err != nil {
		return Watcher[T]{}, err
	}
	return NewWatcher[T](wi), nil
}

type Watcher[T Resource] struct {
	inner watch.Interface
	ch    chan T
}

func NewWatcher[T Resource](wi watch.Interface) Watcher[T] {
	cc := make(chan T)
	go func() {
		for {
			select {
			case res, ok := <-wi.ResultChan():
				if !ok {
					return
				}
				tt, ok := any(res.Object).(*T)
				if !ok {
					return
				}
				cc <- *tt
			}
		}
	}()
	return Watcher[T]{wi, cc}
}

func (w Watcher[T]) Stop() {
	w.inner.Stop()
	close(w.ch)
}

func (w Watcher[T]) Results() <-chan T {
	return w.ch
}

func defaultPath(gv schema.GroupVersionResource) string {
	apiPath := "apis"
	if gv.Group == "" {
		apiPath = "api"
	}
	return rest.DefaultVersionedAPIPath(apiPath, gv.GroupVersion())
}

type Lister[T Resource] interface {
	// List will return all objects across namespaces
	List(selector labels.Selector) []T
	// Get will attempt to retrieve assuming that name==key
	Get(name string) (T, error)
	// ByNamespace will give you a GenericNamespaceLister for one namespace
	ByNamespace(namespace string) NamespaceLister[T]
}

// GenericNamespaceLister is a lister skin on a generic Indexer
type NamespaceLister[T Resource] interface {
	// List will return all objects in this namespace
	List(selector labels.Selector) (ret []T, err error)
	// Get will attempt to retrieve by namespace and name
	Get(name string) (*T, error)
}

type Informer[T Resource] struct {
	api   API[T]
	inner cache.TypedSharedIndexInformer[T]
}

func (i Informer[T]) List(selector labels.Selector) []T {
	return i.inner.GetStore().List()
}

func castList[T any](l []any) []T {
	res := make([]T, 0, len(l))
	for _, v := range l {
		res = append(res, *(v.(*T)))
	}
	return res
}

func (i Informer[T]) Get(name string) (T, error) {
	//TODO implement me
	panic("implement me")
}

func (i Informer[T]) ByNamespace(namespace string) NamespaceLister[T] {
	//TODO implement me
	panic("implement me")
}

func NewInformer[T Resource](api API[T], namespace string) Lister[T] {
	// TODO: make it a factory
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			res, err := api.List(namespace, options)
			if err != nil {
				return nil, err
			}
			return toRuntimeObject(res), nil
			// TODO: convert? Or make our own ListWatch
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			res, err := api.Watch(namespace, options)
			if err != nil {
				return nil, err
			}
			return res.inner, nil
		},
	}
	informer := cache.NewTypedSharedIndexInformer[T](lw, any(new(T)).(runtime.Object), cache.SharedIndexInformerOptions{})
	// Just for simple examples, wouldn't do this in real world
	go informer.Run(make(chan struct{}))
	cache.WaitForCacheSync(make(chan struct{}), informer.HasSynced)
	return Informer[T]{api: api, inner: informer}
}

type GenericList[T Resource] struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is the list of Ts
	Items []T `json:"items" protobuf:"bytes,2,rep,name=items"`
}

func (in GenericList[T]) DeepCopyInto(out *GenericList[T]) {
	*out = GenericList[T]{}
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		items := make([]T, len(in.Items))
		for i := range in.Items {
			cpy := resourceAsobject(in.Items[i]).DeepCopyObject()
			items[i] = cpy.(T)
		}
		out.Items = items
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new StorageClassList.
func (in GenericList[T]) DeepCopy() *GenericList[T] {
	out := new(GenericList[T])
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in GenericList[T]) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func toRuntimeObject[T Resource](res []T) runtime.Object {
	return &GenericList[T]{Items: res}
}

func Name[T Resource]() string {
	pn := &pluralNamer{finalize: strings.ToLower}
	return pn.Name(reflect.TypeOf(*new(T)).Name())
}

type pluralNamer struct {
	// key is the case-sensitive type name, value is the case-insensitive
	// intended output.
	exceptions map[string]string
	finalize   func(string) string
}

// Name returns the plural form of the type's name. If the type's name is found
// in the exceptions map, the map value is returned.
func (r *pluralNamer) Name(singular string) string {
	var plural string
	var ok bool
	if plural, ok = r.exceptions[singular]; ok {
		return r.finalize(plural)
	}
	if len(singular) < 2 {
		return r.finalize(singular)
	}

	switch rune(singular[len(singular)-1]) {
	case 's', 'x', 'z':
		plural = esPlural(singular)
	case 'y':
		sl := rune(singular[len(singular)-2])
		if isConsonant(sl) {
			plural = iesPlural(singular)
		} else {
			plural = sPlural(singular)
		}
	case 'h':
		sl := rune(singular[len(singular)-2])
		if sl == 'c' || sl == 's' {
			plural = esPlural(singular)
		} else {
			plural = sPlural(singular)
		}
	case 'e':
		sl := rune(singular[len(singular)-2])
		if sl == 'f' {
			plural = vesPlural(singular[:len(singular)-1])
		} else {
			plural = sPlural(singular)
		}
	case 'f':
		plural = vesPlural(singular)
	default:
		plural = sPlural(singular)
	}
	return r.finalize(plural)
}

func iesPlural(singular string) string {
	return singular[:len(singular)-1] + "ies"
}

func vesPlural(singular string) string {
	return singular[:len(singular)-1] + "ves"
}

func esPlural(singular string) string {
	return singular + "es"
}

func sPlural(singular string) string {
	return singular + "s"
}

const consonants = "bcdfghjklmnpqrstvwxyz"

func isConsonant(char rune) bool {
	for _, c := range consonants {
		if char == c {
			return true
		}
	}
	return false
}
