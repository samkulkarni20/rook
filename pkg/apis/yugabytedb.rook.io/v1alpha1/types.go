/*
Copyright 2019 The Rook Authors. All rights reserved.

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
package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rook "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
)

// ***************************************************************************
// IMPORTANT FOR CODE GENERATION
// If the types in this file are updated, you will need to run
// `make codegen` to generate the new types under the client/clientset folder.
// ***************************************************************************

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type YugabyteDBCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              YugabyteDBClusterSpec `json:"spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type YugabyteDBClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []YugabyteDBCluster `json:"items"`
}

type YugabyteDBClusterSpec struct {
	Annotations rook.Annotations `json:"annotations,omitempty"`
	Master      ServerSpec       `json:"master"`
	TServer     ServerSpec       `json:"tserver"`
}

type ServerSpec struct {
	Replicas            int32                    `json:"replicas,omitempty"`
	Network             rook.NetworkSpec         `json:"network,omitempty"`
	VolumeClaimTemplate v1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}
