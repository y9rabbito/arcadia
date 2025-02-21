/*
Copyright 2023 KubeAGI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"context"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubeagi/arcadia/api/base/v1alpha1"
	"github.com/kubeagi/arcadia/apiserver/graph/generated"
	"github.com/kubeagi/arcadia/apiserver/pkg/auth"
	"github.com/kubeagi/arcadia/pkg/config"
	"github.com/kubeagi/arcadia/pkg/datasource"
)

var (
	DefaultNamespace  = "default"
	ErrNoResourceKind = errors.New("must provide resource kind")

	// Common status
	StatusTrue  = "True"
	StatusFalse = "False"
)

// ModelType
var (
	ModelTypeAll       = "llm,embedding"
	ModelTypeLLM       = "llm"
	ModelTypeEmbedding = "embedding"
)

// Resource operations

// ResourceGet provides a common way to get a resource
func ResourceGet(ctx context.Context, c dynamic.Interface, resource generated.TypedObjectReferenceInput, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if resource.Namespace == nil {
		resource.Namespace = &DefaultNamespace
	}
	if resource.Kind == "" {
		return nil, ErrNoResourceKind
	}
	return c.Resource(SchemaOf(resource.APIGroup, resource.Kind)).Namespace(*resource.Namespace).Get(ctx, resource.Name, options, subresources...)
}

// ResourceUpdate provides a common way to update a resource
// - resource defines the new object 's apigroup and kind
func ResouceUpdate(ctx context.Context, c dynamic.Interface, resource generated.TypedObjectReferenceInput, newObject map[string]interface{}, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if resource.Namespace == nil {
		resource.Namespace = &DefaultNamespace
	}
	if resource.Kind == "" {
		return nil, ErrNoResourceKind
	}
	return c.Resource(SchemaOf(resource.APIGroup, resource.Kind)).Namespace(*resource.Namespace).Update(ctx, &unstructured.Unstructured{
		Object: newObject,
	}, options, subresources...)
}

func SystemDatasourceOSS(ctx context.Context, mgrClient client.Client, dynamicClient dynamic.Interface) (*datasource.OSS, error) {
	systemDatasource, err := config.GetSystemDatasource(ctx, mgrClient, dynamicClient)
	if err != nil {
		return nil, err
	}
	endpoint := systemDatasource.Spec.Endpoint.DeepCopy()
	if endpoint.AuthSecret != nil && endpoint.AuthSecret.Namespace == nil {
		endpoint.AuthSecret.WithNameSpace(systemDatasource.Namespace)
	}
	return datasource.NewOSS(ctx, mgrClient, dynamicClient, endpoint)
}

// GetAPIServer returns the api server url to access arcadia's worker
// if external is true,then this func will return the external api server
func GetAPIServer(ctx context.Context, dynamicClient dynamic.Interface, external bool) (string, error) {
	gateway, err := config.GetGateway(ctx, nil, dynamicClient)
	if err != nil {
		return "", err
	}
	api := gateway.APIServer
	if external {
		api = gateway.ExternalAPIServer
	}
	return api, nil
}

// GetObjStatus is used to calculate the state of the resource, unified management,
// in general, a resource will only record its own state,
// then the state calculation of this resource, should be written to this function.
// But for some special resources. For example, VersionedDataset,
// he needs to calculate multiple states, it is not suitable for this function.
func GetObjStatus(obj client.Object) string {
	if obj == nil {
		return ""
	}
	if obj.GetDeletionTimestamp() != nil {
		return "Deleting"
	}

	var (
		condition v1alpha1.Condition
	)
	switch obj.GetObjectKind().GroupVersionKind().Kind {
	case "Datasource":
		v := obj.(*v1alpha1.Datasource)
		condition = v.Status.GetCondition(v1alpha1.TypeReady)
	case "Embedder":
		v := obj.(*v1alpha1.Embedder)
		condition = v.Status.GetCondition(v1alpha1.TypeReady)
	case "KnowledgeBase":
		v := obj.(*v1alpha1.KnowledgeBase)
		condition = v.Status.GetCondition(v1alpha1.TypeReady)
	case "LLM":
		v := obj.(*v1alpha1.LLM)
		condition = v.Status.GetCondition(v1alpha1.TypeReady)
	case "Model":
		v := obj.(*v1alpha1.Model)
		condition = v.Status.GetCondition(v1alpha1.TypeReady)
	case "Worker":
		// Worker can better represent the state of resources through Reason.
		v := obj.(*v1alpha1.Worker)
		condition = v.Status.GetCondition(v1alpha1.TypeReady)
		status := string(condition.Reason)
		// When replicas is zero but status is not `Offline`, it must be in `OfflineInProgress`
		if v.Spec.Replicas == nil || *v.Spec.Replicas == 0 {
			if status != "Offline" {
				status = "OfflineInProgress"
			}
		}
		return status
	case "Application":
		v := obj.(*v1alpha1.Application)
		condition = v.Status.GetCondition(v1alpha1.TypeReady)
	default:
		return ""
	}

	return string(condition.Status)
}

// PageNodeConvertFunc convert `any` to a `PageNode`
type PageNodeConvertFunc func(any) generated.PageNode

var (
	DefaultPageNodeConvertFunc = func(node any) generated.PageNode {
		pageNode, ok := node.(generated.PageNode)
		if !ok {
			return nil
		}
		return pageNode
	}
)

var (
	// UnlimitedPageSize which means all
	UnlimitedPageSize = -1
)

// ListOptions for graphql list
type ListOptions struct {
	ConvertFunc PageNodeConvertFunc
}

// DefaultListOptions initialize a ListOptions with default settings
func DefaultListOptions() *ListOptions {
	return &ListOptions{
		ConvertFunc: DefaultPageNodeConvertFunc,
	}
}

type ListOptionsFunc func(options *ListOptions)

// WithPageNodeConvertFunc update the PageNodeConvertFunc
func WithPageNodeConvertFunc(convertFunc PageNodeConvertFunc) ListOptionsFunc {
	return func(option *ListOptions) {
		option.ConvertFunc = convertFunc
	}
}

func SetCreator(ctx context.Context, common *v1alpha1.CommonSpec) {
	currentUser, _ := ctx.Value(auth.UserNameContextKey).(string)
	common.Creator = currentUser
}

func PagePosition(page, size, total int) (int, int) {
	// if pageSize is -1 which means unlimited pageSize,return all
	if size == UnlimitedPageSize {
		page = 1
		size = total
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := page * size
	if end > total {
		end = total
	}
	return start, end
}
