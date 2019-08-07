/*
Copyright 2018 The Rook Authors. All rights reserved.

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

package installer

import "strconv"

type YugabyteDBManifests struct {
}

func (_ *YugabyteDBManifests) GetYugabyteDBOperatorSpecs(namespace string) string {
	return `apiVersion: v1
kind: Namespace
metadata:
  name: ` + namespace + `
---
apiVersion: rbac.authorization.k8s.io/v1beta1
kind: ClusterRole
metadata:
  name: rook-yugabytedb-operator
rules:
- apiGroups:
  - ""
  resources:
  - pods
  verbs:
  - get
  - list
- apiGroups:
  - ""
  resources:
  - services
  verbs:
  - get
  - list
  - watch
  - create
  - update
  - patch
  - delete
- apiGroups:
  - apps
  resources:
  - statefulsets
  verbs:
  - get
  - list
  - watch
  - create
  - update
  - patch
  - delete
- apiGroups:
  - yugabytedb.rook.io
  resources:
  - "*"
  verbs:
  - "*"
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: rook-yugabytedb-operator
  namespace: ` + namespace + `
---
apiVersion: rbac.authorization.k8s.io/v1beta1
kind: ClusterRoleBinding
metadata:
  name: rook-yugabytedb-operator
  namespace: ` + namespace + `
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: rook-yugabytedb-operator
subjects:
- kind: ServiceAccount
  name: rook-yugabytedb-operator
  namespace: ` + namespace + `
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rook-yugabytedb-operator
  namespace: ` + namespace + `
  labels:
    app: rook-yugabytedb-operator
spec:
  selector:
    matchLabels:
      app: rook-yugabytedb-operator
  replicas: 1
  template:
    metadata:
      labels:
        app: rook-yugabytedb-operator
    spec:
      serviceAccountName: rook-yugabytedb-operator
      containers:
      - name: rook-yugabytedb-operator
        image: samkulkarni20/rook-yugabytedb:latest
        args: ["yugabytedb", "operator"]
        env:
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace`
}

func (_ *YugabyteDBManifests) GetYugabyteDBCRDSpecs() string {
	return `apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: clusters.yugabytedb.rook.io
spec:
  group: yugabytedb.rook.io
  names:
    kind: Cluster
    listKind: ClusterList
    singular: cluster
    plural: clusters
  scope: Namespaced
  version: v1alpha1`
}

func (_ *YugabyteDBManifests) GetYugabyteDBClusterSpecs(namespace string, replicaCount int) string {
	return `apiVersion: yugabytedb.rook.io/v1alpha1
kind: Cluster
metadata:
  name: rook-yugabytedb
  namespace: ` + namespace + `
spec:
  # Mentioning network ports is optional. If some or all ports are not specified, then they will be defaulted to below mentioned values, except for tserver-ui.
  # For tserver-ui a cluster ip service will be created if the yb-tserver-ui port is explicitly mentioned. If it is not specified, only StatefulSet & headless service will be created for TServer.
  # TServer ClusterIP service creation will be skipped. Whereas for Master, all 3 kubernetes objects will always be created.
  network:
    ports:
      - name: yb-master-ui
        port: 7000          # default value
      - name: yb-master-grpc
        port: 7100          # default value
      - name: yb-tserver-ui
        port: 9001          # default value
      - name: yb-tserver-grpc
        port: 9100          # default value
      - name: yb-tserver-cassandra
        port: 9042          # default value
      - name: yb-tserver-redis
        port: 6379          # default value
      - name: yb-tserver-postgres
        port: 5433          # default value
  # Replica count for master & tserver
  replicas:
    master: ` + strconv.Itoa(replicaCount) + `
    tserver: ` + strconv.Itoa(replicaCount) + `
  scope:
    master:
      volumeClaimTemplates:
      - metadata:
          name: datadir
        spec:
          accessModes: [ "ReadWriteOnce" ]
          resources:
            requests:
              storage: 10Mi
          storageClassName: standard
    tserver:
      volumeClaimTemplates:
      - metadata:
          name: datadir
        spec:
          accessModes: [ "ReadWriteOnce" ]
          resources:
            requests:
              storage: 10Mi
          storageClassName: standard`
}
