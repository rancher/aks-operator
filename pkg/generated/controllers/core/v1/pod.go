/*
Copyright 2019 Wrangler Sample Controller Authors

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

// Code generated by main. DO NOT EDIT.

package v1

import (
	"context"
	"time"

	"github.com/rancher/wrangler/v2/pkg/apply"
	"github.com/rancher/wrangler/v2/pkg/condition"
	"github.com/rancher/wrangler/v2/pkg/generic"
	"github.com/rancher/wrangler/v2/pkg/kv"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// PodController interface for managing Pod resources.
type PodController interface {
	generic.ControllerInterface[*v1.Pod, *v1.PodList]
}

// PodClient interface for managing Pod resources in Kubernetes.
type PodClient interface {
	generic.ClientInterface[*v1.Pod, *v1.PodList]
}

// PodCache interface for retrieving Pod resources in memory.
type PodCache interface {
	generic.CacheInterface[*v1.Pod]
}

type PodStatusHandler func(obj *v1.Pod, status v1.PodStatus) (v1.PodStatus, error)

type PodGeneratingHandler func(obj *v1.Pod, status v1.PodStatus) ([]runtime.Object, v1.PodStatus, error)

func RegisterPodStatusHandler(ctx context.Context, controller PodController, condition condition.Cond, name string, handler PodStatusHandler) {
	statusHandler := &podStatusHandler{
		client:    controller,
		condition: condition,
		handler:   handler,
	}
	controller.AddGenericHandler(ctx, name, generic.FromObjectHandlerToHandler(statusHandler.sync))
}

func RegisterPodGeneratingHandler(ctx context.Context, controller PodController, apply apply.Apply,
	condition condition.Cond, name string, handler PodGeneratingHandler, opts *generic.GeneratingHandlerOptions) {
	statusHandler := &podGeneratingHandler{
		PodGeneratingHandler: handler,
		apply:                apply,
		name:                 name,
		gvk:                  controller.GroupVersionKind(),
	}
	if opts != nil {
		statusHandler.opts = *opts
	}
	controller.OnChange(ctx, name, statusHandler.Remove)
	RegisterPodStatusHandler(ctx, controller, condition, name, statusHandler.Handle)
}

type podStatusHandler struct {
	client    PodClient
	condition condition.Cond
	handler   PodStatusHandler
}

func (a *podStatusHandler) sync(key string, obj *v1.Pod) (*v1.Pod, error) {
	if obj == nil {
		return obj, nil
	}

	origStatus := obj.Status.DeepCopy()
	obj = obj.DeepCopy()
	newStatus, err := a.handler(obj, obj.Status)
	if err != nil {
		// Revert to old status on error
		newStatus = *origStatus.DeepCopy()
	}

	if a.condition != "" {
		if errors.IsConflict(err) {
			a.condition.SetError(&newStatus, "", nil)
		} else {
			a.condition.SetError(&newStatus, "", err)
		}
	}
	if !equality.Semantic.DeepEqual(origStatus, &newStatus) {
		if a.condition != "" {
			// Since status has changed, update the lastUpdatedTime
			a.condition.LastUpdated(&newStatus, time.Now().UTC().Format(time.RFC3339))
		}

		var newErr error
		obj.Status = newStatus
		newObj, newErr := a.client.UpdateStatus(obj)
		if err == nil {
			err = newErr
		}
		if newErr == nil {
			obj = newObj
		}
	}
	return obj, err
}

type podGeneratingHandler struct {
	PodGeneratingHandler
	apply apply.Apply
	opts  generic.GeneratingHandlerOptions
	gvk   schema.GroupVersionKind
	name  string
}

func (a *podGeneratingHandler) Remove(key string, obj *v1.Pod) (*v1.Pod, error) {
	if obj != nil {
		return obj, nil
	}

	obj = &v1.Pod{}
	obj.Namespace, obj.Name = kv.RSplit(key, "/")
	obj.SetGroupVersionKind(a.gvk)

	return nil, generic.ConfigureApplyForObject(a.apply, obj, &a.opts).
		WithOwner(obj).
		WithSetID(a.name).
		ApplyObjects()
}

func (a *podGeneratingHandler) Handle(obj *v1.Pod, status v1.PodStatus) (v1.PodStatus, error) {
	if !obj.DeletionTimestamp.IsZero() {
		return status, nil
	}

	objs, newStatus, err := a.PodGeneratingHandler(obj, status)
	if err != nil {
		return newStatus, err
	}

	return newStatus, generic.ConfigureApplyForObject(a.apply, obj, &a.opts).
		WithOwner(obj).
		WithSetID(a.name).
		ApplyObjects(objs...)
}
