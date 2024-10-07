//go:build expansion
// +build expansion

package generics

import (
	"context"
	"io"
	"strings"

	v1 "k8s.io/api/core/v1"
)

type ResourceLogger interface {
	Resource
	// TODO: add constraint into types
	//IsLogger()
}

func GetLogs[T ResourceLogger](c *Client, name, namespace string, opts v1.PodLogOptions) (string, error) {
	gv := resourceMetadata[T](c)
	res, err := c.client.Get().
		Namespace(namespace).
		Resource(gv.Resource).
		SubResource("log").
		Name(name).
		VersionedParams(&opts, c.parameterCodec).
		AbsPath(defaultPath(gv)).
		Stream(context.Background())
	if err != nil {
		return "", nil
	}
	defer func() {
		res.Close()
	}()

	builder := &strings.Builder{}
	if _, err = io.Copy(builder, res); err != nil {
		return "", err
	}

	return builder.String(), nil
}
