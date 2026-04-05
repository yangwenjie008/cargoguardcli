package k8s

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

// ==================== Info Structs ====================

// PodInfo Pod 信息
type PodInfo struct {
	Name              string
	Namespace         string
	Status            string
	NodeName          string
	HostIP            string
	PodIP             string
	Ready             string
	Restarts          int
	Age               time.Duration
	CreationTimestamp time.Time
	Labels            map[string]string
}

// DeploymentInfo Deployment 信息
type DeploymentInfo struct {
	Name              string
	Namespace         string
	Replicas          int
	AvailableReplicas int
	ReadyReplicas     int
	Image             string
	Age               time.Duration
	Labels            map[string]string
}

// ServiceInfo Service 信息
type ServiceInfo struct {
	Name      string
	Namespace string
	Type      string
	ClusterIP string
	Ports     string
	Age       time.Duration
}

// ConfigMapInfo ConfigMap 信息
type ConfigMapInfo struct {
	Name              string
	Namespace         string
	DataCount        int
	Immutable         bool
	CreationTimestamp time.Time
}

// SecretInfo Secret 信息
type SecretInfo struct {
	Name              string
	Namespace         string
	Type              string
	DataCount         int
	CreationTimestamp time.Time
}

// NodeInfo Node 信息
type NodeInfo struct {
	Name       string
	Status     string
	Roles      string
	Age        time.Duration
	Labels     map[string]string
	Allocatable corev1.ResourceList
}

// NamespaceInfo Namespace 信息
type NamespaceInfo struct {
	Name              string
	Status            string
	Labels            map[string]string
	CreationTimestamp time.Time
}

// EventInfo Event 信息
type EventInfo struct {
	Name      string
	Namespace string
	Type      string
	Reason    string
	Message   string
	Count     int
	Age       time.Duration
}

// ClusterInfo 集群信息
type ClusterInfo struct {
	ServerVersion  string
	Platform       string
	NodeCount      int
	NamespaceCount int
}
