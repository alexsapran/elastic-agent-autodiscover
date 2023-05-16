// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package metadata

import (
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/elastic/elastic-agent-autodiscover/kubernetes"
	"github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

type pod struct {
	store               cache.Store
	client              k8s.Interface
	node                MetaGen
	replicaset          MetaGen
	job                 MetaGen
	resource            *Resource
	addResourceMetadata *AddResourceMetadataConfig
}

// NewPodMetadataGenerator creates a metagen for pod resources
func NewPodMetadataGenerator(
	cfg *config.C,
	pods cache.Store,
	client k8s.Interface,
	node MetaGen,
	namespace MetaGen,
	replicaset MetaGen,
	job MetaGen,
	addResourceMetadata *AddResourceMetadataConfig) MetaGen {

	return &pod{
		resource:            NewNamespaceAwareResourceMetadataGenerator(cfg, client, namespace),
		store:               pods,
		node:                node,
		replicaset:          replicaset,
		job:                 job,
		client:              client,
		addResourceMetadata: addResourceMetadata,
	}
}

// Generate generates pod metadata from a resource object
// Metadata map is in the following form:
//
//	{
//	      "kubernetes": {},
//	   "some.ecs.field": "asdf"
//	}
//
// All Kubernetes fields that need to be stored under kubernetes. prefix are populated by
// GenerateK8s method while fields that are part of ECS are generated by GenerateECS method
func (p *pod) Generate(obj kubernetes.Resource, opts ...FieldOptions) mapstr.M {
	ecsFields := p.GenerateECS(obj)
	meta := mapstr.M{
		"kubernetes": p.GenerateK8s(obj, opts...),
	}
	meta.DeepUpdate(ecsFields)
	return meta
}

// GenerateECS generates pod ECS metadata from a resource object
func (p *pod) GenerateECS(obj kubernetes.Resource) mapstr.M {
	return p.resource.GenerateECS(obj)
}

// GenerateK8s generates pod metadata from a resource object
func (p *pod) GenerateK8s(obj kubernetes.Resource, opts ...FieldOptions) mapstr.M {
	po, ok := obj.(*kubernetes.Pod)
	if !ok {
		return nil
	}

	out := p.resource.GenerateK8s("pod", obj, opts...)

	// check if Pod is handled by a ReplicaSet which is controlled by a Deployment.
	// The hierarchy there is Deployment->ReplicaSet->Pod.
	if p.addResourceMetadata.Deployment {
		if p.replicaset != nil {
			rsName, _ := out.GetValue("replicaset.name")
			if rsName, ok := rsName.(string); ok {
				meta := p.replicaset.GenerateFromName(rsName)
				deploymentName, _ := meta.GetValue("deployment.name")
				if deploymentName != "" {
					_, _ = out.Put("deployment.name", deploymentName)
				}
			}
		}
	}

	// check if Pod is handled by a Job which is controlled by a CronJob.
	// The hierarchy there is CronJob->Job->Pod
	if p.addResourceMetadata.CronJob {
		if p.job != nil {
			jobName, _ := out.GetValue("job.name")
			if jobName, ok := jobName.(string); ok {
				meta := p.replicaset.GenerateFromName(jobName)
				cronjobName, _ := meta.GetValue("cronjob.name")
				if cronjobName != "" {
					_, _ = out.Put("cronjob.name", cronjobName)
				}
			}
		}
	}

	if p.node != nil {
		meta := p.node.GenerateFromName(po.Spec.NodeName, WithMetadata("node"))
		if meta != nil {
			_, _ = out.Put("node", meta["node"])
		} else {
			_, _ = out.Put("node.name", po.Spec.NodeName)
		}
	} else {
		_, _ = out.Put("node.name", po.Spec.NodeName)
	}

	if po.Status.PodIP != "" {
		_, _ = out.Put("pod.ip", po.Status.PodIP)
	}

	return out
}

// GenerateFromName generates pod metadata from a pod name
func (p *pod) GenerateFromName(name string, opts ...FieldOptions) mapstr.M {
	if p.store == nil {
		return nil
	}

	if obj, ok, _ := p.store.GetByKey(name); ok {
		po, ok := obj.(*kubernetes.Pod)
		if !ok {
			return nil
		}

		return p.GenerateK8s(po, opts...)
	}

	return nil
}
