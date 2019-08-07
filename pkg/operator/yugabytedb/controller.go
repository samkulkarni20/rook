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

// Package yugabytedb to manage a yugabytedb cluster.
package yugabytedb

import (
	"fmt"
	"reflect"
	"time"

	opkit "github.com/rook/operator-kit"
	rookv1alpha2 "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
	yugabytedbv1alpha1 "github.com/rook/rook/pkg/apis/yugabytedb.rook.io/v1alpha1"
	"github.com/rook/rook/pkg/clusterd"
	"github.com/rook/rook/pkg/operator/k8sutil"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
)

const (
	CustomResourceName                = "cluster"
	CustomResourceNamePlural          = "clusters"
	MasterName                        = "yb-master"
	MasterNamePlural                  = "yb-masters"
	TServerName                       = "yb-tserver"
	TServerNamePlural                 = "yb-tservers"
	MasterUIServiceName               = "yb-master-ui"
	TServerUIServiceName              = "yb-tserver-ui"
	createInitRetryIntervalDefault    = 6 * time.Second
	MasterUIPortDefault               = int32(7000)
	MasterUIPortName                  = "yb-master-ui"
	MasterRPCPortDefault              = int32(7100)
	MasterRPCPortName                 = "yb-master-grpc"
	TServerUIPortDefault              = int32(9000)
	TServerUIPortName                 = "yb-tserver-ui"
	TServerRPCPortDefault             = int32(9100)
	TServerRPCPortName                = "yb-tserver-grpc"
	TServerCassandraPortDefault       = int32(9042)
	TServerCassandraPortName          = "yb-tserver-cassandra"
	TServerRedisPortDefault           = int32(6379)
	TServerRedisPortName              = "yb-tserver-redis"
	TServerPostgresPortDefault        = int32(5433)
	TServerPostgresPortName           = "yb-tserver-postgres"
	MasterContainerUIPortName         = "master-ui"
	MasterContainerRPCPortName        = "master-rpc"
	TServerContainerUIPortName        = "tserver-ui"
	TServerContainerRPCPortName       = "tserver-rpc"
	TServerContainerCassandraPortName = "cassandra"
	TServerContainerRedisPortName     = "redis"
	TServerContainerPostgresPortName  = "postgres"
	UIPortName                        = "ui"
	RPCPortName                       = "rpc-port"
	CassandraPortName                 = "cassandra"
	RedisPortName                     = "redis"
	PostgresPortName                  = "postgres"
	VolumeDataName                    = "datadir"
	VolumeMountPath                   = "/mnt/data0"
	envGetHostsFrom                   = "GET_HOSTS_FROM"
	envGetHostsFromVal                = "dns"
	envPodIP                          = "POD_IP"
	envPodIPVal                       = "status.podIP"
	envPodName                        = "POD_NAME"
	envPodNameVal                     = "metadata.name"
)

var ClusterResource = opkit.CustomResource{
	Name:    CustomResourceName,
	Plural:  CustomResourceNamePlural,
	Group:   yugabytedbv1alpha1.CustomResourceGroup,
	Version: yugabytedbv1alpha1.Version,
	Scope:   apiextensionsv1beta1.NamespaceScoped,
	Kind:    reflect.TypeOf(yugabytedbv1alpha1.Cluster{}).Name(),
}

type ClusterController struct {
	context                 *clusterd.Context
	containerImage          string
	// TODO: Remove createInitRetryInterval if not used
	createInitRetryInterval time.Duration
}

func NewClusterController(context *clusterd.Context, containerImage string) *ClusterController {
	return &ClusterController{
		context:                 context,
		containerImage:          containerImage,
		// TODO: Remove createInitRetryInterval if not used
		createInitRetryInterval: createInitRetryIntervalDefault,
	}
}

type cluster struct {
	context     *clusterd.Context
	namespace   string
	spec        yugabytedbv1alpha1.ClusterSpec
	annotations rookv1alpha2.Annotations
	ownerRef    metav1.OwnerReference
}

type clusterPorts struct {
	masterPorts, tserverPorts serverPorts
}

type serverPorts struct {
	ui, rpc, cassandra, redis, postgres int32
}

func newCluster(c *yugabytedbv1alpha1.Cluster, context *clusterd.Context) *cluster {
	return &cluster{
		context:     context,
		namespace:   c.Namespace,
		spec:        c.Spec,
		annotations: c.Spec.Annotations,
		ownerRef:    clusterOwnerRef(c.Namespace, string(c.UID)),
	}
}

func clusterOwnerRef(namespace, clusterID string) metav1.OwnerReference {
	blockOwner := true
	return metav1.OwnerReference{
		APIVersion:         fmt.Sprintf("%s/%s", ClusterResource.Group, ClusterResource.Version),
		Kind:               ClusterResource.Kind,
		Name:               namespace,
		UID:                types.UID(clusterID),
		BlockOwnerDeletion: &blockOwner,
	}
}

func (c *ClusterController) StartWatch(namespace string, stopCh chan struct{}) error {
	resourceHandlerFuncs := cache.ResourceEventHandlerFuncs{
		AddFunc:    c.onAdd,
		UpdateFunc: c.onUpdate,
		DeleteFunc: c.onDelete,
	}

	logger.Infof("start watching yugabytedb clusters in all namespaces")
	watcher := opkit.NewWatcher(ClusterResource, namespace, resourceHandlerFuncs, c.context.RookClientset.YugabytedbV1alpha1().RESTClient())
	go watcher.Watch(&yugabytedbv1alpha1.Cluster{}, stopCh)

	return nil
}

func (c *ClusterController) onAdd(obj interface{}) {
	// TODO Cleanup resources if something fails in between.
	clusterObj := obj.(*yugabytedbv1alpha1.Cluster).DeepCopy()
	logger.Infof("new cluster %s added to namespace %s", clusterObj.Name, clusterObj.Namespace)

	cluster := newCluster(clusterObj, c.context)

	if err := validateClusterSpec(cluster.spec); err != nil {
		logger.Errorf("invalid cluster spec: %+v", err)
		return
	}

	if err := c.createMasterHeadlessService(cluster); err != nil {
		logger.Errorf("failed to create master headless service: %+v", err)
		return
	}

	if err := c.createTServerHeadlessService(cluster); err != nil {
		logger.Errorf("failed to create TServer headless service: %+v", err)
		return
	}

	if err := c.createMasterUIService(cluster); err != nil {
		logger.Errorf("failed to create Master UI service: %+v", err)
		return
	}

	if err := c.createTServerUIService(cluster); err != nil {
		logger.Errorf("failed to create replica service: %+v", err)
		return
	}

	if err := c.createMasterStatefulset(cluster); err != nil {
		logger.Errorf("failed to create master stateful set: %+v", err)
		return
	}

	if err := c.createTServerStatefulset(cluster); err != nil {
		logger.Errorf("failed to create tserver stateful set: %+v", err)
		return
	}

	logger.Infof("succeeded creating and initializing cluster in namespace %s", cluster.namespace)
}

func (c *ClusterController) onUpdate(oldObj, newObj interface{}) {
	_ = oldObj.(*yugabytedbv1alpha1.Cluster).DeepCopy()
	newObjCluster := newObj.(*yugabytedbv1alpha1.Cluster).DeepCopy()
	newYBCluster := newCluster(newObjCluster, c.context)

	// Validate new spec
	if err := validateClusterSpec(newYBCluster.spec); err != nil {
		logger.Errorf("invalid cluster spec: %+v", err)
		return
	}

	// Update headless service ports
	if err := c.updateMasterHeadlessService(newYBCluster); err != nil {
		logger.Errorf("failed to update Master headless service: %+v", err)
		return
	}

	if err := c.updateTServerHeadlessService(newYBCluster); err != nil {
		logger.Errorf("failed to update TServer headless service: %+v", err)
		return
	}

	// Create/update/delete UI services (create/delete would apply for TServer UI services)
	if err := c.updateMasterUIService(newYBCluster); err != nil {
		logger.Errorf("failed to update Master UI service: %+v", err)
		return
	}

	if err := c.updateTServerUIService(newYBCluster); err != nil {
		logger.Errorf("failed to update TServer UI service: %+v", err)
		return
	}

	// Update StatefulSets replica count, command, ports & PVCs.
	if err := c.updateMasterStatefulset(newYBCluster); err != nil {
		logger.Errorf("failed to update Master statefulsets: %+v", err)
		return
	}

	if err := c.updateTServerStatefulset(newYBCluster); err != nil {
		logger.Errorf("failed to update TServer statefulsets: %+v", err)
		return
	}

	logger.Infof("cluster %s updated in namespace %s", newObjCluster.Name, newObjCluster.Namespace)
}

func (c *ClusterController) onDelete(obj interface{}) {
	cluster, ok := obj.(*yugabytedbv1alpha1.Cluster)
	if !ok {
		return
	}
	cluster = cluster.DeepCopy()
	logger.Infof("cluster %s deleted from namespace %s", cluster.Name, cluster.Namespace)
}

func (c *ClusterController) createMasterUIService(cluster *cluster) error {
	return c.createUIService(cluster, false)
}

// Create UI service for TServer, if user has specified a UI port for it. Do not create it implicitly, with default port.
func (c *ClusterController) createTServerUIService(cluster *cluster) error {
	return c.createUIService(cluster, true)
}

func (c *ClusterController) createUIService(cluster *cluster, isTServerService bool) error {
	ports, err := getPortsFromSpec(cluster.spec.Network)
	if err != nil {
		return err
	}

	serviceName := MasterUIServiceName
	label := MasterName

	if isTServerService {
		// If user hasn't specified TServer UI port, do not create a UI service for it.
		if ports.tserverPorts.ui <= 0 {
			return nil
		}

		serviceName = TServerUIServiceName
		label = TServerName
	}

	// This service is meant to be used by clients of the database. It exposes a ClusterIP that will
	// automatically load balance connections to the different database pods.
	uiService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: cluster.namespace,
			Labels:    createAppLabels(label),
		},
		Spec: v1.ServiceSpec{
			Selector: createAppLabels(label),
			Type:     v1.ServiceTypeClusterIP,
			Ports:    createUIServicePorts(ports, isTServerService),
		},
	}
	k8sutil.SetOwnerRef(&uiService.ObjectMeta, &cluster.ownerRef)

	if _, err := c.context.Clientset.CoreV1().Services(cluster.namespace).Create(uiService); err != nil {
		if !errors.IsAlreadyExists(err) {
			return err
		}
		logger.Infof("client service %s already exists in namespace %s", uiService.Name, uiService.Namespace)
	} else {
		logger.Infof("client service %s started in namespace %s", uiService.Name, uiService.Namespace)
	}

	return nil
}

func (c *ClusterController) updateMasterUIService(newCluster *cluster) error {
	return c.updateUIService(newCluster, false)
}

// Update/Delete UI service for TServer, if user has specified/removed a UI port for it.
func (c *ClusterController) updateTServerUIService(newCluster *cluster) error {
	return c.updateUIService(newCluster, true)
}

func (c *ClusterController) updateUIService(newCluster *cluster, isTServerService bool) error {
	ports, err := getPortsFromSpec(newCluster.spec.Network)
	if err != nil {
		return err
	}

	serviceName := MasterUIServiceName

	if isTServerService {
		serviceName = TServerUIServiceName
	}

	service, err := c.context.Clientset.CoreV1().Services(newCluster.namespace).Get(serviceName, metav1.GetOptions{})

	if err != nil {
		// Create TServer UI Service, if it wasn't present & new spec needs one.
		if errors.IsNotFound(err) && isTServerService {
			// The below condition is not clubbed with other two above, so as to
			// report the error if any of above conditions is false; irrespective of TServer UI port value in the new spec.
			if ports.tserverPorts.ui > 0 {
				return c.createTServerUIService(newCluster)
			}

			// Return if TServer UI service did not exist previously & new spec also doesn't need one to be created.
			return nil
		}

		return err
	}

	// Delete the TServer UI service if existed, but new spec doesn't need one.
	if isTServerService && service != nil && ports.tserverPorts.ui <= 0 {
		if err := c.context.Clientset.CoreV1().Services(newCluster.namespace).Delete(serviceName, &metav1.DeleteOptions{}); err != nil {
			return err
		}

		logger.Infof("UI service %s deleted in namespace %s", service.Name, service.Namespace)

		return nil
	}

	// Update the UI service for Master or TServer, otherwise.
	service.Spec.Ports = createUIServicePorts(ports, isTServerService)

	if _, err := c.context.Clientset.CoreV1().Services(newCluster.namespace).Update(service); err != nil {
		return err
	}

	logger.Infof("client service %s updated in namespace %s", service.Name, service.Namespace)

	return nil
}

func (c *ClusterController) createMasterHeadlessService(cluster *cluster) error {
	return c.createHeadlessService(cluster, false)
}

func (c *ClusterController) createTServerHeadlessService(cluster *cluster) error {
	return c.createHeadlessService(cluster, true)
}

func (c *ClusterController) createHeadlessService(cluster *cluster, isTServerService bool) error {
	ports, err := getPortsFromSpec(cluster.spec.Network)

	if err != nil {
		return err
	}

	serviceName := MasterNamePlural
	label := MasterName

	if isTServerService {
		serviceName = TServerNamePlural
		label = TServerName
	}

	// This service only exists to create DNS entries for each pod in the stateful
	// set such that they can resolve each other's IP addresses. It does not
	// create a load-balanced ClusterIP and should not be used directly by clients
	// in most circumstances.
	headlessService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: cluster.namespace,
			Labels:    createAppLabels(label),
		},
		Spec: v1.ServiceSpec{
			Selector: createAppLabels(label),
			// We want all pods in the StatefulSet to have their addresses published for
			// the sake of the other YugabyteDB pods even before they're ready, since they
			// have to be able to talk to each other in order to become ready.
			ClusterIP: "None",
			Ports:     createServicePorts(ports, isTServerService),
		},
	}

	k8sutil.SetOwnerRef(&headlessService.ObjectMeta, &cluster.ownerRef)

	if _, err := c.context.Clientset.CoreV1().Services(cluster.namespace).Create(headlessService); err != nil {
		if !errors.IsAlreadyExists(err) {
			return err
		}
		logger.Infof("headless service %s already exists in namespace %s", headlessService.Name, headlessService.Namespace)
	} else {
		logger.Infof("headless service %s started in namespace %s", headlessService.Name, headlessService.Namespace)
	}

	return nil
}

func (c *ClusterController) updateMasterHeadlessService(newCluster *cluster) error {
	return c.updateHeadlessService(newCluster, false)
}

func (c *ClusterController) updateTServerHeadlessService(newCluster *cluster) error {
	return c.updateHeadlessService(newCluster, true)
}

func (c *ClusterController) updateHeadlessService(newCluster *cluster, isTServerService bool) error {
	ports, err := getPortsFromSpec(newCluster.spec.Network)

	if err != nil {
		return err
	}

	serviceName := MasterNamePlural

	if isTServerService {
		serviceName = TServerNamePlural
	}

	service, err := c.context.Clientset.CoreV1().Services(newCluster.namespace).Get(serviceName, metav1.GetOptions{})

	if err != nil {
		return err
	}

	service.Spec.Ports = createServicePorts(ports, isTServerService)

	if _, err := c.context.Clientset.CoreV1().Services(newCluster.namespace).Update(service); err != nil {
		return err
	}

	logger.Infof("headless service %s updated in namespace %s", service.Name, service.Namespace)

	return nil
}

func (c *ClusterController) createMasterStatefulset(cluster *cluster) error {
	return c.createStatefulSet(cluster, false)
}

func (c *ClusterController) createTServerStatefulset(cluster *cluster) error {
	return c.createStatefulSet(cluster, true)
}

func (c *ClusterController) createStatefulSet(cluster *cluster, isTServerStatefulset bool) error {
	replicas := int32(cluster.spec.Replicas.Master)
	appName := MasterName
	appNamePlural := MasterNamePlural
	volumeClaimTemplate := cluster.spec.Storage.Master.VolumeClaimTemplates

	if isTServerStatefulset {
		replicas = int32(cluster.spec.Replicas.TServer)
		appName = TServerName
		appNamePlural = TServerNamePlural
		volumeClaimTemplate = cluster.spec.Storage.TServer.VolumeClaimTemplates
	}

	ports, err := getPortsFromSpec(cluster.spec.Network)
	if err != nil {
		return err
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: cluster.namespace,
			Labels:    createAppLabels(appName),
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName:         appNamePlural,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Replicas:            &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: createAppLabels(appName),
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.namespace,
					Labels:    createAppLabels(appName),
				},
				Spec: createPodSpec(cluster, c.containerImage, ports, isTServerStatefulset, appName),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			VolumeClaimTemplates: volumeClaimTemplate,
		},
	}
	cluster.annotations.ApplyToObjectMeta(&statefulSet.Spec.Template.ObjectMeta)
	cluster.annotations.ApplyToObjectMeta(&statefulSet.ObjectMeta)
	k8sutil.SetOwnerRef(&statefulSet.ObjectMeta, &cluster.ownerRef)

	if _, err := c.context.Clientset.AppsV1().StatefulSets(cluster.namespace).Create(statefulSet); err != nil {
		if !errors.IsAlreadyExists(err) {
			return err
		}
		logger.Infof("stateful set %s already exists in namespace %s", statefulSet.Name, statefulSet.Namespace)
	} else {
		logger.Infof("stateful set %s created in namespace %s", statefulSet.Name, statefulSet.Namespace)
	}

	return nil
}

func (c *ClusterController) updateMasterStatefulset(newCluster *cluster) error {
	return c.updateStatefulSet(newCluster, false)
}

func (c *ClusterController) updateTServerStatefulset(newCluster *cluster) error {
	return c.updateStatefulSet(newCluster, true)
}

func (c *ClusterController) updateStatefulSet(newCluster *cluster, isTServerStatefulset bool) error {
	ports, err := getPortsFromSpec(newCluster.spec.Network)

	if err != nil {
		return err
	}

	replicas := int32(newCluster.spec.Replicas.Master)
	sfsName := MasterName
	volumeClaimTemplate := newCluster.spec.Storage.Master.VolumeClaimTemplates
	command := createMasterContainerCommand(ports.masterPorts.rpc, newCluster.spec.Replicas.Master)
	containerPorts := createMasterContainerPortsList(ports)

	if isTServerStatefulset {
		replicas = int32(newCluster.spec.Replicas.TServer)
		sfsName = TServerName
		volumeClaimTemplate = newCluster.spec.Storage.TServer.VolumeClaimTemplates
		command = createTServerContainerCommand(ports.masterPorts.rpc, ports.tserverPorts.rpc, ports.tserverPorts.postgres, newCluster.spec.Replicas.TServer)
		containerPorts = createTServerContainerPortsList(ports)
	}

	sfs, err := c.context.Clientset.AppsV1().StatefulSets(newCluster.namespace).Get(sfsName, metav1.GetOptions{})

	if err != nil {
		return err
	}

	sfs.Spec.Replicas = &replicas
	sfs.Spec.Template.Spec.Containers[0].Command = command
	sfs.Spec.Template.Spec.Containers[0].Ports = containerPorts
	sfs.Spec.VolumeClaimTemplates = volumeClaimTemplate

	if _, err := c.context.Clientset.AppsV1().StatefulSets(newCluster.namespace).Update(sfs); err != nil {
		return err
	} else {
		logger.Infof("stateful set %s updated in namespace %s", sfs.Name, sfs.Namespace)
	}

	return nil
}

func createPodSpec(cluster *cluster, containerImage string, ports *clusterPorts, isTServerStatefulset bool, appName string) v1.PodSpec {
	return v1.PodSpec{
		Affinity: &v1.Affinity{
			PodAntiAffinity: &v1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{
					{
						Weight: int32(100),
						PodAffinityTerm: v1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      k8sutil.AppAttr,
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{appName},
									},
								},
							},
							TopologyKey: v1.LabelHostname,
						},
					},
				},
			},
		},
		Containers: []v1.Container{createContainer(cluster, containerImage, ports, isTServerStatefulset, appName)},
	}
}

func createContainer(cluster *cluster, containerImage string, ports *clusterPorts, isTServerStatefulset bool, appName string) v1.Container {
	command := createMasterContainerCommand(ports.masterPorts.rpc, cluster.spec.Replicas.Master)
	containerPorts := createMasterContainerPortsList(ports)

	if isTServerStatefulset {
		command = createTServerContainerCommand(ports.masterPorts.rpc, ports.tserverPorts.rpc, ports.tserverPorts.postgres, cluster.spec.Replicas.TServer)
		containerPorts = createTServerContainerPortsList(ports)
	}

	return v1.Container{
		Name:            appName,
		Image:           "yugabytedb/yugabyte:latest",
		ImagePullPolicy: v1.PullAlways,
		Env: []v1.EnvVar{
			{
				Name:  envGetHostsFrom,
				Value: envGetHostsFromVal,
			},
			{
				Name: envPodIP,
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: envPodIPVal,
					},
				},
			},
			{
				Name: envPodName,
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: envPodNameVal,
					},
				},
			},
		},
		Command: command,
		Ports:   containerPorts,
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      VolumeDataName,
				MountPath: VolumeMountPath,
			},
		},
	}
}

func validateClusterSpec(spec yugabytedbv1alpha1.ClusterSpec) error {

	if spec.Replicas.Master < 1 {
		return fmt.Errorf("invalid Master replica count: %d. Must be at least 1", spec.Replicas.Master)
	}

	if spec.Replicas.TServer < 1 {
		return fmt.Errorf("invalid TServer replica count: %d. Must be at least 1", spec.Replicas.TServer)
	}

	if _, err := getPortsFromSpec(spec.Network); err != nil {
		return err
	}

	if &spec.Storage.Master == nil {
		return fmt.Errorf("Master storage spec not found.")
	}

	if &spec.Storage.Master.VolumeClaimTemplates == nil {
		return fmt.Errorf("VolumeClaimTemplate unavailable in Master storage spec.")
	}

	if &spec.Storage.TServer == nil {
		return fmt.Errorf("TServer storage spec not found.")
	}

	if &spec.Storage.TServer.VolumeClaimTemplates == nil {
		return fmt.Errorf("VolumeClaimTemplate unavailable in TServer storage spec.")
	}

	return nil
}

func createAppLabels(label string) map[string]string {
	return map[string]string{
		k8sutil.AppAttr: label,
	}
}

func createServicePorts(ports *clusterPorts, isTServerService bool) []v1.ServicePort {
	var servicePorts []v1.ServicePort

	if !isTServerService {
		servicePorts = []v1.ServicePort{
			{
				Name:       UIPortName,
				Port:       ports.masterPorts.ui,
				TargetPort: intstr.FromInt(int(ports.masterPorts.ui)),
			},
			{
				Name:       RPCPortName,
				Port:       ports.masterPorts.rpc,
				TargetPort: intstr.FromInt(int(ports.masterPorts.rpc)),
			},
		}
	} else {
		tserverUIPort := ports.tserverPorts.ui

		if tserverUIPort <= 0 {
			tserverUIPort = TServerUIPortDefault
		}

		servicePorts = []v1.ServicePort{
			{
				Name:       UIPortName,
				Port:       tserverUIPort,
				TargetPort: intstr.FromInt(int(tserverUIPort)),
			},
			{
				Name:       RPCPortName,
				Port:       ports.tserverPorts.rpc,
				TargetPort: intstr.FromInt(int(ports.tserverPorts.rpc)),
			},
			{
				Name:       CassandraPortName,
				Port:       ports.tserverPorts.cassandra,
				TargetPort: intstr.FromInt(int(ports.tserverPorts.cassandra)),
			},
			{
				Name:       RedisPortName,
				Port:       ports.tserverPorts.redis,
				TargetPort: intstr.FromInt(int(ports.tserverPorts.redis)),
			},
			{
				Name:       PostgresPortName,
				Port:       ports.tserverPorts.postgres,
				TargetPort: intstr.FromInt(int(ports.tserverPorts.postgres)),
			},
		}
	}

	return servicePorts
}

func createUIServicePorts(ports *clusterPorts, isTServerService bool) []v1.ServicePort {
	var servicePorts []v1.ServicePort

	if !isTServerService {
		servicePorts = []v1.ServicePort{
			{
				Name:       UIPortName,
				Port:       ports.masterPorts.ui,
				TargetPort: intstr.FromInt(int(ports.masterPorts.ui)),
			},
		}
	} else {
		if ports.tserverPorts.ui > 0 {
			servicePorts = []v1.ServicePort{
				{
					Name:       UIPortName,
					Port:       ports.tserverPorts.ui,
					TargetPort: intstr.FromInt(int(ports.tserverPorts.ui)),
				},
			}
		} else {
			servicePorts = nil
		}
	}

	return servicePorts
}

func getPortsFromSpec(networkSpec rookv1alpha2.NetworkSpec) (clusterPort *clusterPorts, err error) {
	ports := clusterPorts{}

	for _, p := range networkSpec.Ports {
		switch p.Name {
		case MasterUIPortName:
			ports.masterPorts.ui = p.Port
		case MasterRPCPortName:
			ports.masterPorts.rpc = p.Port
		case TServerUIPortName:
			ports.tserverPorts.ui = p.Port
		case TServerRPCPortName:
			ports.tserverPorts.rpc = p.Port
		case TServerCassandraPortName:
			ports.tserverPorts.cassandra = p.Port
		case TServerRedisPortName:
			ports.tserverPorts.redis = p.Port
		case TServerPostgresPortName:
			ports.tserverPorts.postgres = p.Port
		default:
			return &clusterPorts{}, fmt.Errorf("Invalid port name: %s. Must be one of: [%s, %s, %s, %s, %s, %s, %s]", p.Name,
				MasterUIPortName, MasterRPCPortName, TServerUIPortName, TServerRPCPortName, TServerCassandraPortName,
				TServerRedisPortName, TServerPostgresPortName)
		}
	}

	if ports.masterPorts.ui == 0 {
		ports.masterPorts.ui = MasterUIPortDefault
	}

	if ports.masterPorts.rpc == 0 {
		ports.masterPorts.rpc = MasterRPCPortDefault
	}

	if ports.tserverPorts.rpc == 0 {
		ports.tserverPorts.rpc = TServerRPCPortDefault
	}

	if ports.tserverPorts.cassandra == 0 {
		ports.tserverPorts.cassandra = TServerCassandraPortDefault
	}

	if ports.tserverPorts.redis == 0 {
		ports.tserverPorts.redis = TServerRedisPortDefault
	}

	if ports.tserverPorts.postgres == 0 {
		ports.tserverPorts.postgres = TServerPostgresPortDefault
	}

	return &ports, nil
}

func createMasterContainerCommand(grpcPort, replicas int32) []string {
	command := []string{
		"/home/yugabyte/bin/yb-master",
		"--fs_data_dirs=/mnt/data0",
		fmt.Sprintf("--rpc_bind_addresses=$(POD_IP):%d", grpcPort),
		fmt.Sprintf("--server_broadcast_addresses=$(POD_NAME).yb-masters:%d", grpcPort),
		"--use_private_ip=never",
		fmt.Sprintf("--master_addresses=yb-masters.default.svc.cluster.local:%d", grpcPort),
		fmt.Sprintf("--master_replication_factor=%d", replicas),
		"--logtostderr",
	}
	return command
}

func createTServerContainerCommand(masterGRPCPort, tserverGRPCPort, pgsqlPort, replicas int32) []string {
	command := []string{
		"/home/yugabyte/bin/yb-tserver",
		"--fs_data_dirs=/mnt/data0",
		fmt.Sprintf("--rpc_bind_addresses=$(POD_IP):%d", tserverGRPCPort),
		fmt.Sprintf("--server_broadcast_addresses=$(POD_NAME).yb-tservers:%d", tserverGRPCPort),
		"--start_pgsql_proxy",
		fmt.Sprintf("--pgsql_proxy_bind_address=$(POD_IP):%d", pgsqlPort),
		"--use_private_ip=never",
		fmt.Sprintf("--tserver_master_addrs=yb-masters.default.svc.cluster.local:%d", masterGRPCPort),
		fmt.Sprintf("--tserver_master_replication_factor=%d", replicas),
		"--logtostderr",
	}
	return command
}

func createMasterContainerPortsList(clusterPortsSpec *clusterPorts) []v1.ContainerPort {
	ports := []v1.ContainerPort{
		{
			Name:          MasterContainerUIPortName,
			ContainerPort: int32(clusterPortsSpec.masterPorts.ui),
		},
		{
			Name:          MasterContainerRPCPortName,
			ContainerPort: int32(clusterPortsSpec.masterPorts.rpc),
		},
	}

	return ports
}

func createTServerContainerPortsList(clusterPortsSpec *clusterPorts) []v1.ContainerPort {
	tserverUIPort := int32(clusterPortsSpec.tserverPorts.ui)

	if tserverUIPort <= 0 {
		tserverUIPort = TServerUIPortDefault
	}

	ports := []v1.ContainerPort{
		{
			Name:          TServerContainerUIPortName,
			ContainerPort: tserverUIPort,
		},
		{
			Name:          TServerContainerRPCPortName,
			ContainerPort: int32(clusterPortsSpec.tserverPorts.rpc),
		},
		{
			Name:          TServerContainerCassandraPortName,
			ContainerPort: int32(clusterPortsSpec.tserverPorts.cassandra),
		},
		{
			Name:          TServerContainerRedisPortName,
			ContainerPort: int32(clusterPortsSpec.tserverPorts.redis),
		},
		{
			Name:          TServerContainerPostgresPortName,
			ContainerPort: int32(clusterPortsSpec.tserverPorts.postgres),
		},
	}

	return ports
}
