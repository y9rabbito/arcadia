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
package scheduler

import (
	"context"

	"github.com/KawashiroNitori/butcher/v2"
	"github.com/kubeagi/arcadia/api/v1alpha1"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Scheduler struct {
	ctx    context.Context
	cancel context.CancelFunc
	runner butcher.Butcher
	client client.Client

	ds *v1alpha1.VersionedDataset
}

func NewScheduler(ctx context.Context, c client.Client, instance *v1alpha1.VersionedDataset) (*Scheduler, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx1, cancel := context.WithCancel(ctx)
	s := &Scheduler{ctx: ctx1, cancel: cancel, ds: instance, client: c}
	exectuor, err := butcher.NewButcher[JobPayload](newExecutor(ctx1, c, instance), butcher.BufferSize(bufSize), butcher.MaxWorker(maxWorkers))
	if err != nil {
		return nil, err
	}
	s.runner = exectuor
	return s, nil
}

func (s *Scheduler) Start() error {
	if err := s.runner.Run(s.ctx); err != nil {
		klog.Errorf("versionDataset %s/%s run failed err %s.", s.ds.Namespace, s.ds.Name, err)
		return err
	}

	// Only when there are no errors, the latest CR is fetched to check if the resource has changed.
	ds := &v1alpha1.VersionedDataset{}
	if err := s.client.Get(s.ctx, types.NamespacedName{Namespace: s.ds.Namespace, Name: s.ds.Name}, ds); err != nil {
		klog.Errorf("versionDataset %s/%s get failed. err %s", s.ds.Namespace, s.ds.Name, err)
		return err
	}

	if ds.DeletionTimestamp != nil {
		ds.Finalizers = nil
		klog.Infof("versionDataset %s/%s is being deleted, so we need to update his finalizers to allow the deletion to complete smoothly", ds.Namespace, ds.Name)
		return s.client.Update(s.ctx, ds)
	}

	if ds.ResourceVersion == s.ds.ResourceVersion {
		deepCopy := ds.DeepCopy()
		deepCopy.Status.DatasourceFiles = s.ds.Status.DatasourceFiles
		return s.client.Status().Patch(s.ctx, deepCopy, client.MergeFrom(ds))
	}

	klog.Infof("current resourceversion: %s, previous resourceversion: %s", ds.ResourceVersion, s.ds.ResourceVersion)
	return nil
}

func (s *Scheduler) Stop() {
	s.cancel()
}
