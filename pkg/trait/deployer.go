/*
Licensed to the Apache Software Foundation (ASF) under one or more
contributor license agreements.  See the NOTICE file distributed with
this work for additional information regarding copyright ownership.
The ASF licenses this file to You under the Apache License, Version 2.0
(the "License"); you may not use this file except in compliance with
the License.  You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package trait

import (
	jsonpatch "github.com/evanphx/json-patch"

	"github.com/pkg/errors"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/apache/camel-k/pkg/apis/camel/v1alpha1"
	"github.com/apache/camel-k/pkg/util/kubernetes"
)

// The deployer trait can be used to explicitly select the kind of high level resource that
// will deploy the integration.
//
// +camel-k:trait=deployer
type deployerTrait struct {
	BaseTrait `property:",squash"`
	// Allows to explicitly select the desired deployment kind between `deployment` or `knative-service` when creating the resources for running the integration.
	Kind string `property:"kind"`
}

func newDeployerTrait() *deployerTrait {
	return &deployerTrait{
		BaseTrait: newBaseTrait("deployer"),
	}
}

func (t *deployerTrait) Configure(e *Environment) (bool, error) {
	return e.IntegrationInPhase(
		v1alpha1.IntegrationPhaseInitialization,
		v1alpha1.IntegrationPhaseDeploying,
		v1alpha1.IntegrationPhaseRunning,
	), nil
}

func (t *deployerTrait) Apply(e *Environment) error {
	switch e.Integration.Status.Phase {

	case v1alpha1.IntegrationPhaseInitialization, v1alpha1.IntegrationPhaseDeploying:
		// Register a post action that updates the resources generated by the traits
		e.PostActions = append(e.PostActions, func(env *Environment) error {
			if err := kubernetes.ReplaceResources(env.C, env.Client, env.Resources.Items()); err != nil {
				return errors.Wrap(err, "error during replace resource")
			}
			return nil
		})

	case v1alpha1.IntegrationPhaseRunning:
		// Register a post action that patches the resources generated by the traits
		e.PostActions = append(e.PostActions, func(env *Environment) error {
			for _, resource := range env.Resources.Items() {
				key, err := client.ObjectKeyFromObject(resource)
				if err != nil {
					return err
				}

				object := resource.DeepCopyObject()
				err = env.Client.Get(env.C, key, object)
				if err != nil {
					return err
				}

				err = env.Client.Patch(env.C, resource, mergeFrom(object))
				if err != nil {
					return errors.Wrap(err, "error during patch resource")
				}
			}
			return nil
		})
	}

	return nil
}

// IsPlatformTrait overrides base class method
func (t *deployerTrait) IsPlatformTrait() bool {
	return true
}

type mergeFromPositivePatch struct {
	from runtime.Object
}

func (s *mergeFromPositivePatch) Type() types.PatchType {
	return types.MergePatchType
}

func (s *mergeFromPositivePatch) Data(obj runtime.Object) ([]byte, error) {
	originalJSON, err := json.Marshal(s.from)
	if err != nil {
		return nil, err
	}

	modifiedJSON, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}

	patch, err := jsonpatch.CreateMergePatch(originalJSON, modifiedJSON)
	if err != nil {
		return nil, err
	}

	// The following is a work-around to remove null fields from the JSON merge patch
	// so that values defaulted by controllers server-side are not deleted.
	// It's generally acceptable as these values are orthogonal to the values managed
	// by the traits.
	out := obj.DeepCopyObject()
	err = json.Unmarshal(patch, out)
	if err != nil {
		return nil, err
	}

	return json.Marshal(out)
}

func mergeFrom(obj runtime.Object) client.Patch {
	return &mergeFromPositivePatch{obj}
}
