package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// Client K8s 纯 Go 客户端（不依赖 kubectl CLI）
type Client struct {
	clientset  *kubernetes.Clientset
	dynClient  dynamic.Interface
	config     *rest.Config
	kubeconfig string
	context    string
	namespace  string
}

// ClientConfig 客户端配置
type ClientConfig struct {
	Kubeconfig string // kubeconfig 路径，默认 ~/.kube/config
	Context    string // context 名称
	Namespace  string // 默认 namespace，默认 "default"
}

// NewClient 创建 K8s 纯 Go 客户端
func NewClient(cfg ClientConfig) (*Client, error) {
	c := &Client{
		kubeconfig: cfg.Kubeconfig,
		context:    cfg.Context,
		namespace:  cfg.Namespace,
	}

	// 设置默认值
	if c.kubeconfig == "" {
		home, _ := os.UserHomeDir()
		c.kubeconfig = home + "/.kube/config"
	}
	if c.namespace == "" {
		c.namespace = "default"
	}

	// 构建 clientcmd 配置
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.ExplicitPath = c.kubeconfig

	overrides := &clientcmd.ConfigOverrides{}
	if c.context != "" {
		overrides.CurrentContext = c.context
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build client config: %w", err)
	}

	// 创建 clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	// 创建 dynamic client
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	c.clientset = clientset
	c.dynClient = dynClient
	c.config = config

	return c, nil
}

// ==================== Namespaces ====================

// ListNamespaces 列出所有 namespaces
func (c *Client) ListNamespaces(ctx context.Context) ([]string, error) {
	namespaces, err := c.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	result := make([]string, 0, len(namespaces.Items))
	for _, ns := range namespaces.Items {
		result = append(result, ns.Name)
	}
	return result, nil
}

// GetNamespaceInfo 获取 namespace 信息
func (c *Client) GetNamespaceInfo(ctx context.Context, name string) (*NamespaceInfo, error) {
	ns, err := c.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get namespace: %w", err)
	}

	return &NamespaceInfo{
		Name:              ns.Name,
		Status:            string(ns.Status.Phase),
		Labels:            ns.Labels,
		CreationTimestamp: ns.CreationTimestamp.Time,
	}, nil
}

// ==================== Pods ====================

// ListPods 列出 pods
func (c *Client) ListPods(ctx context.Context, namespace, labelSelector string) ([]interface{}, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	opts := metav1.ListOptions{}
	if labelSelector != "" {
		opts.LabelSelector = labelSelector
	}

	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	result := make([]interface{}, 0, len(pods.Items))
	for _, pod := range pods.Items {
		result = append(result, podToMap(&pod))
	}
	return result, nil
}

// podToMap 将 Pod 转换为 map
func podToMap(pod *corev1.Pod) map[string]interface{} {
	return map[string]interface{}{
		"name":      pod.Name,
		"namespace": pod.Namespace,
		"status":    string(pod.Status.Phase),
		"node":      pod.Spec.NodeName,
		"hostIP":    pod.Status.HostIP,
		"podIP":     pod.Status.PodIP,
		"ready":     formatPodReady(pod),
		"restarts":  countRestarts(pod),
		"age":       formatAge(pod.CreationTimestamp.Time),
		"labels":    pod.Labels,
	}
}

// formatPodReady 格式化 Ready 状态
func formatPodReady(pod *corev1.Pod) string {
	ready := 0
	total := len(pod.Spec.Containers)
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
	}
	return fmt.Sprintf("%d/%d", ready, total)
}

// countRestarts 统计重启次数
func countRestarts(pod *corev1.Pod) int {
	total := 0
	for _, cs := range pod.Status.ContainerStatuses {
		total += int(cs.RestartCount)
	}
	return total
}

// formatAge 格式化时间
func formatAge(t time.Time) string {
	d := time.Since(t)
	if d.Hours() >= 24 {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	if d.Minutes() >= 60 {
		return fmt.Sprintf("%dh", int(d.Minutes()/60))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// WatchPods 监听 pods 变化
func (c *Client) WatchPods(ctx context.Context, namespace, labelSelector string, handler func(string, interface{})) error {
	if namespace == "" {
		namespace = c.namespace
	}

	opts := metav1.ListOptions{}
	if labelSelector != "" {
		opts.LabelSelector = labelSelector
	}

	watcher, err := c.clientset.CoreV1().Pods(namespace).Watch(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to watch pods: %w", err)
	}

	fmt.Println("Watching pods... (Press Ctrl+C to stop)")

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return nil
			}
			handler(string(event.Type), event.Object)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// GetPod 获取单个 Pod
func (c *Client) GetPod(ctx context.Context, namespace, name string) (*PodInfo, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	pod, err := c.clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	return &PodInfo{
		Name:              pod.Name,
		Namespace:         pod.Namespace,
		Status:            string(pod.Status.Phase),
		NodeName:          pod.Spec.NodeName,
		HostIP:            pod.Status.HostIP,
		PodIP:             pod.Status.PodIP,
		Ready:             formatPodReady(pod),
		Restarts:          countRestarts(pod),
		Age:               time.Since(pod.CreationTimestamp.Time),
		CreationTimestamp: pod.CreationTimestamp.Time,
		Labels:            pod.Labels,
	}, nil
}

// GetPodLogs 获取 pod 日志（纯 Go 实现）
func (c *Client) GetPodLogs(ctx context.Context, namespace, name, container string, tailLines int64) (string, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	opts := &corev1.PodLogOptions{}
	if container != "" {
		opts.Container = container
	}
	if tailLines > 0 {
		opts.TailLines = &tailLines
	}

	req := c.clientset.CoreV1().RESTClient().Get().
		Namespace(namespace).
		Name(name).
		Resource("pods").
		SubResource("log").
		VersionedParams(opts, metav1.ParameterCodec)

	logs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get pod logs: %w", err)
	}
	defer logs.Close()

	body, err := io.ReadAll(logs)
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}

	return string(body), nil
}

// DeletePod 删除 pod
func (c *Client) DeletePod(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = c.namespace
	}

	err := c.clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete pod: %w", err)
	}
	return nil
}

// ==================== Deployments ====================

// ListDeployments 列出 deployments
func (c *Client) ListDeployments(ctx context.Context, namespace string) ([]DeploymentInfo, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	deps, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	result := make([]DeploymentInfo, 0, len(deps.Items))
	for _, dep := range deps.Items {
		replicas := int32(0)
		if dep.Spec.Replicas != nil {
			replicas = *dep.Spec.Replicas
		}
		result = append(result, DeploymentInfo{
			Name:              dep.Name,
			Namespace:         dep.Namespace,
			Replicas:          int(replicas),
			AvailableReplicas: int(dep.Status.AvailableReplicas),
			ReadyReplicas:     int(dep.Status.ReadyReplicas),
			Image:             getDeploymentImage(&dep),
			Age:               time.Since(dep.CreationTimestamp.Time),
			Labels:            dep.Labels,
		})
	}
	return result, nil
}

// getDeploymentImage 获取 deployment 的容器镜像
func getDeploymentImage(dep *appsv1.Deployment) string {
	if len(dep.Spec.Template.Spec.Containers) > 0 {
		return dep.Spec.Template.Spec.Containers[0].Image
	}
	return ""
}

// ScaleDeployment 扩缩容
func (c *Client) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	if namespace == "" {
		namespace = c.namespace
	}

	scale, err := c.clientset.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment scale: %w", err)
	}

	scale.Spec.Replicas = replicas
	_, err = c.clientset.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale deployment: %w", err)
	}

	return nil
}

// RestartDeployment 重启 deployment（纯 Go 实现）
func (c *Client) RestartDeployment(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = c.namespace
	}

	// 获取当前 deployment
	dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// 更新 annotations 触发滚动重启
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = make(map[string]string)
	}
	dep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	_, err = c.clientset.AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to restart deployment: %w", err)
	}

	fmt.Printf("✅ Deployment %s/%s restarting...\n", namespace, name)
	return nil
}

// ==================== Services ====================

// ListServices 列出 services
func (c *Client) ListServices(ctx context.Context, namespace string) ([]ServiceInfo, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	svcs, err := c.clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	result := make([]ServiceInfo, 0, len(svcs.Items))
	for _, svc := range svcs.Items {
		result = append(result, ServiceInfo{
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Type:      string(svc.Spec.Type),
			ClusterIP: svc.Spec.ClusterIP,
			Ports:     formatServicePorts(&svc),
			Age:       time.Since(svc.CreationTimestamp.Time),
		})
	}
	return result, nil
}

// formatServicePorts 格式化服务端口
func formatServicePorts(svc *corev1.Service) string {
	var ports []string
	for _, p := range svc.Spec.Ports {
		ports = append(ports, fmt.Sprintf("%d:%d/%s", p.Port, p.NodePort, p.Protocol))
	}
	return strings.Join(ports, ",")
}

// ==================== ConfigMaps & Secrets ====================

// ListConfigMaps 列出 configmaps
func (c *Client) ListConfigMaps(ctx context.Context, namespace string) ([]ConfigMapInfo, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	cms, err := c.clientset.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list configmaps: %w", err)
	}

	result := make([]ConfigMapInfo, 0, len(cms.Items))
	for _, cm := range cms.Items {
		result = append(result, ConfigMapInfo{
			Name:              cm.Name,
			Namespace:         cm.Namespace,
			DataCount:        len(cm.Data),
			Immutable:         cm.Immutable != nil && *cm.Immutable,
			CreationTimestamp: cm.CreationTimestamp.Time,
		})
	}
	return result, nil
}

// ListSecrets 列出 secrets
func (c *Client) ListSecrets(ctx context.Context, namespace string) ([]SecretInfo, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	secrets, err := c.clientset.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets: %w", err)
	}

	result := make([]SecretInfo, 0, len(secrets.Items))
	for _, s := range secrets.Items {
		result = append(result, SecretInfo{
			Name:              s.Name,
			Namespace:         s.Namespace,
			Type:              string(s.Type),
			DataCount:         len(s.Data),
			CreationTimestamp: s.CreationTimestamp.Time,
		})
	}
	return result, nil
}

// ==================== Nodes ====================

// ListNodes 列出 nodes
func (c *Client) ListNodes(ctx context.Context) ([]NodeInfo, error) {
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	result := make([]NodeInfo, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		result = append(result, NodeInfo{
			Name:        node.Name,
			Status:      formatNodeStatus(&node),
			Roles:       getNodeRoles(&node),
			Age:         time.Since(node.CreationTimestamp.Time),
			Labels:      node.Labels,
			Allocatable: node.Status.Allocatable,
		})
	}
	return result, nil
}

// formatNodeStatus 格式化节点状态
func formatNodeStatus(node *corev1.Node) string {
	conditions := node.Status.Conditions
	for _, cond := range conditions {
		if cond.Type == corev1.NodeReady {
			if cond.Status == corev1.ConditionTrue {
				return "Ready"
			}
			return "NotReady"
		}
	}
	return "Unknown"
}

// getNodeRoles 获取节点角色
func getNodeRoles(node *corev1.Node) string {
	var roles []string
	for k := range node.Labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			roles = append(roles, strings.TrimPrefix(k, "node-role.kubernetes.io/"))
		}
	}
	if len(roles) == 0 {
		roles = append(roles, "<none>")
	}
	return strings.Join(roles, ",")
}

// ==================== Events ====================

// ListEvents 列出 events
func (c *Client) ListEvents(ctx context.Context, namespace string) ([]EventInfo, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	events, err := c.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}

	result := make([]EventInfo, 0, len(events.Items))
	for _, e := range events.Items {
		result = append(result, EventInfo{
			Name:      e.Name,
			Namespace: e.Namespace,
			Type:      e.Type,
			Reason:    e.Reason,
			Message:   e.Message,
			Count:     int(e.Count),
			Age:       time.Since(e.LastTimestamp.Time),
		})
	}
	return result, nil
}

// ==================== Generic Resource Operations (YAML) ====================

// parseResourceKind 解析资源类型
func parseResourceKind(resourceType string) (apiGroup, resource string, namespaced bool) {
	resource = strings.ToLower(resourceType)

	switch resource {
	case "pods", "po":
		resource = "pods"
		namespaced = true
	case "deployments", "deploy":
		apiGroup = "apps"
		resource = "deployments"
		namespaced = true
	case "services", "svc":
		resource = "services"
		namespaced = true
	case "configmaps", "cm":
		resource = "configmaps"
		namespaced = true
	case "secrets", "sec":
		resource = "secrets"
		namespaced = true
	case "namespaces", "ns":
		resource = "namespaces"
		namespaced = false
	case "nodes", "no":
		resource = "nodes"
		namespaced = false
	case "serviceaccounts", "sa":
		resource = "serviceaccounts"
		namespaced = true
	case "persistentvolumes", "pv":
		resource = "persistentvolumes"
		namespaced = false
	case "persistentvolumeclaims", "pvc":
		resource = "persistentvolumeclaims"
		namespaced = true
	case "jobs":
		resource = "jobs"
		namespaced = true
	case "cronjobs":
		apiGroup = "batch"
		resource = "cronjobs"
		namespaced = true
	case "daemonsets", "ds":
		apiGroup = "apps"
		resource = "daemonsets"
		namespaced = true
	case "statefulsets", "sts":
		apiGroup = "apps"
		resource = "statefulsets"
		namespaced = true
	case "ingresses", "ing":
		apiGroup = "networking.k8s.io"
		resource = "ingresses"
		namespaced = true
	case "networkpolicies", "netpol":
		apiGroup = "networking.k8s.io"
		resource = "networkpolicies"
		namespaced = true
	case "horizontalpodautoscalers", "hpa":
		apiGroup = "autoscaling"
		resource = "horizontalpodautoscalers"
		namespaced = true
	case "endpoints", "ep":
		resource = "endpoints"
		namespaced = true
	default:
		namespaced = true
	}

	return
}

// Apply 应用 YAML manifest（纯 Go 实现）
func (c *Client) Apply(ctx context.Context, manifest string) error {
	obj := &unstructured.Unstructured{}
	_, _, err := scheme.Codecs.UniversalDeserializer().Decode([]byte(manifest), nil, obj)
	if err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	apiGroup, resource, namespaced := parseResourceKind(obj.GetKind())
	if apiGroup == "" {
		apiGroup = obj.GroupVersionKind().Group
	}

	namespace := obj.GetNamespace()
	if namespace == "" && namespaced {
		namespace = c.namespace
	}
	if namespace != "" {
		obj.SetNamespace(namespace)
	}

	gvr := schema.GroupVersionResource{
		Group:    apiGroup,
		Version:  "v1",
		Resource: resource,
	}

	var result *unstructured.Unstructured
	if namespaced {
		existing, err := c.dynClient.Resource(gvr).Namespace(namespace).Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			result, err = c.dynClient.Resource(gvr).Namespace(namespace).Create(ctx, obj, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create resource: %w", err)
			}
			fmt.Printf("✅ Created %s/%s\n", result.GetKind(), result.GetName())
		} else {
			obj.SetResourceVersion(existing.GetResourceVersion())
			result, err = c.dynClient.Resource(gvr).Namespace(namespace).Update(ctx, obj, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update resource: %w", err)
			}
			fmt.Printf("✅ Updated %s/%s\n", result.GetKind(), result.GetName())
		}
	} else {
		existing, err := c.dynClient.Resource(gvr).Get(ctx, obj.GetName(), metav1.GetOptions{})
		if err != nil {
			result, err = c.dynClient.Resource(gvr).Create(ctx, obj, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create resource: %w", err)
			}
			fmt.Printf("✅ Created %s/%s\n", result.GetKind(), result.GetName())
		} else {
			obj.SetResourceVersion(existing.GetResourceVersion())
			result, err = c.dynClient.Resource(gvr).Update(ctx, obj, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update resource: %w", err)
			}
			fmt.Printf("✅ Updated %s/%s\n", result.GetKind(), result.GetName())
		}
	}

	return nil
}

// ApplyFile 应用 YAML 文件
func (c *Client) ApplyFile(ctx context.Context, filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	docs := splitYAMLDocuments(string(data))
	for i, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		if len(docs) > 1 {
			fmt.Printf("--- Processing document %d/%d ---\n", i+1, len(docs))
		}
		if err := c.Apply(ctx, doc); err != nil {
			return fmt.Errorf("failed to apply document %d: %w", i+1, err)
		}
	}
	return nil
}

// splitYAMLDocuments 分割多文档 YAML
func splitYAMLDocuments(content string) []string {
	var docs []string
	lines := strings.Split(content, "\n")
	var current strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "---") {
			if current.Len() > 0 {
				docs = append(docs, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteString(line + "\n")
	}

	if current.Len() > 0 {
		docs = append(docs, current.String())
	}

	return docs
}

// Delete 删除资源（纯 Go 实现）
func (c *Client) Delete(ctx context.Context, resourceType, name, namespace string) error {
	apiGroup, resource, namespaced := parseResourceKind(resourceType)

	gvr := schema.GroupVersionResource{
		Group:    apiGroup,
		Version:  "v1",
		Resource: resource,
	}

	if namespace == "" && namespaced {
		namespace = c.namespace
	}

	var err error
	if namespaced && namespace != "" {
		err = c.dynClient.Resource(gvr).Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	} else {
		err = c.dynClient.Resource(gvr).Delete(ctx, name, metav1.DeleteOptions{})
	}

	if err != nil {
		return fmt.Errorf("failed to delete %s/%s: %w", resourceType, name, err)
	}

	fmt.Printf("✅ Deleted %s/%s\n", resourceType, name)
	return nil
}

// Get 获取资源（纯 Go 实现）
func (c *Client) Get(ctx context.Context, resourceType, name, namespace string) (string, error) {
	apiGroup, resource, namespaced := parseResourceKind(resourceType)

	gvr := schema.GroupVersionResource{
		Group:    apiGroup,
		Version:  "v1",
		Resource: resource,
	}

	if namespace == "" && namespaced {
		namespace = c.namespace
	}

	var obj *unstructured.Unstructured
	var err error

	if namespaced && namespace != "" {
		obj, err = c.dynClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = c.dynClient.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	}

	if err != nil {
		return "", fmt.Errorf("failed to get %s/%s: %w", resourceType, name, err)
	}

	data, _ := json.MarshalIndent(obj.Object, "", "  ")
	return string(data), nil
}

// Describe 获取资源详情（纯 Go 实现）
func (c *Client) Describe(ctx context.Context, resourceType, name, namespace string) (string, error) {
	apiGroup, resource, namespaced := parseResourceKind(resourceType)

	gvr := schema.GroupVersionResource{
		Group:    apiGroup,
		Version:  "v1",
		Resource: resource,
	}

	if namespace == "" && namespaced {
		namespace = c.namespace
	}

	var obj *unstructured.Unstructured
	var err error

	if namespaced && namespace != "" {
		obj, err = c.dynClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = c.dynClient.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	}

	if err != nil {
		return "", fmt.Errorf("failed to get %s/%s: %w", resourceType, name, err)
	}

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("Name:         %s\n", obj.GetName()))
	buf.WriteString(fmt.Sprintf("Namespace:    %s\n", obj.GetNamespace()))
	buf.WriteString(fmt.Sprintf("Labels:       %v\n", obj.GetLabels()))
	buf.WriteString(fmt.Sprintf("Annotations:  %v\n", obj.GetAnnotations()))
	buf.WriteString(fmt.Sprintf("Created:      %s\n", obj.GetCreationTimestamp().Time.Format(time.RFC3339)))
	buf.WriteString(fmt.Sprintf("Resource Version: %s\n", obj.GetResourceVersion()))

	if spec, ok := obj.Object["spec"].(map[string]interface{}); ok {
		buf.WriteString("\nSpec:\n")
		for k, v := range spec {
			buf.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
		}
	}

	if status, ok := obj.Object["status"].(map[string]interface{}); ok {
		buf.WriteString("\nStatus:\n")
		for k, v := range status {
			buf.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
		}
	}

	return buf.String(), nil
}

// Exec 在 Pod 中执行命令（纯 Go 实现）
func (c *Client) Exec(ctx context.Context, namespace, pod, container string, command []string) (string, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	req := c.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command:   command,
			Container: container,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, metav1.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("failed to create executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	if err != nil {
		return "", fmt.Errorf("exec failed: %w, stderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// PortForward 端口转发（纯 Go 实现）
// 注意：完整实现需要 k8s.io/client-go/tools/portforward
// 这里提供简化版本，实际生产环境建议使用专门的端口转发工具
func (c *Client) PortForward(ctx context.Context, namespace, pod string, localPort, remotePort int) error {
	fmt.Printf("Port forward: localhost:%d -> pod:%d\n", localPort, remotePort)
	fmt.Println("提示：使用 'kubectl port-forward' 命令获得完整功能")
	
	return nil
}

// ==================== Cluster Info ====================

// GetClusterInfo 获取集群信息
func (c *Client) GetClusterInfo(ctx context.Context) (map[string]string, error) {
	version, err := c.clientset.Discovery().ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get server version: %w", err)
	}

	nodes, _ := c.ListNodes(ctx)
	namespaces, _ := c.ListNamespaces(ctx)

	info := map[string]string{
		"version":  version.GitVersion,
		"platform": version.Platform,
		"context":  c.context,
	}

	if nodes != nil {
		info["nodes"] = fmt.Sprintf("%d", len(nodes))
	}
	if namespaces != nil {
		info["namespaces"] = fmt.Sprintf("%d", len(namespaces))
	}

	return info, nil
}

// SetNamespace 设置默认 namespace
func (c *Client) SetNamespace(ns string) {
	c.namespace = ns
}

// GetNamespace 获取当前 namespace
func (c *Client) GetNamespace() string {
	return c.namespace
}

// GetClientset 获取原始 clientset
func (c *Client) GetClientset() *kubernetes.Clientset {
	return c.clientset
}

// GetDynamicClient 获取 dynamic client
func (c *Client) GetDynamicClient() dynamic.Interface {
	return c.dynClient
}

// ==================== Additional CRUD Operations ====================

// CreateNamespace 创建 namespace
func (c *Client) CreateNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	_, err := c.clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	return err
}

// DeleteNamespace 删除 namespace
func (c *Client) DeleteNamespace(ctx context.Context, name string) error {
	return c.clientset.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
}

// GetConfigMap 获取 configmap
func (c *Client) GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error) {
	if namespace == "" {
		namespace = c.namespace
	}
	return c.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
}

// UpdateConfigMap 更新 configmap
func (c *Client) UpdateConfigMap(ctx context.Context, namespace string, cm *corev1.ConfigMap) error {
	if namespace == "" {
		namespace = c.namespace
	}
	_, err := c.clientset.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

// DeleteConfigMap 删除 configmap
func (c *Client) DeleteConfigMap(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = c.namespace
	}
	return c.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// GetSecret 获取 secret
func (c *Client) GetSecret(ctx context.Context, namespace, name string) (*corev1.Secret, error) {
	if namespace == "" {
		namespace = c.namespace
	}
	return c.clientset.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
}

// DeleteSecret 删除 secret
func (c *Client) DeleteSecret(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = c.namespace
	}
	return c.clientset.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListServiceAccounts 列出 serviceaccounts
func (c *Client) ListServiceAccounts(ctx context.Context, namespace string) ([]corev1.ServiceAccount, error) {
	if namespace == "" {
		namespace = c.namespace
	}
	saList, err := c.clientset.CoreV1().ServiceAccounts(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return saList.Items, nil
}

// ListIngresses 列出 ingresses
func (c *Client) ListIngresses(ctx context.Context, namespace string) ([]interface{}, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	gvr := schema.GroupVersionResource{
		Group:    "networking.k8s.io",
		Version:  "v1",
		Resource: "ingresses",
	}

	unstructuredList, err := c.dynClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list ingresses: %w", err)
	}

	result := make([]interface{}, 0, len(unstructuredList.Items))
	for _, item := range unstructuredList.Items {
		result = append(result, item.Object)
	}
	return result, nil
}

// ListPersistentVolumeClaims 列出 PVCs
func (c *Client) ListPersistentVolumeClaims(ctx context.Context, namespace string) ([]corev1.PersistentVolumeClaim, error) {
	if namespace == "" {
		namespace = c.namespace
	}
	pvcList, err := c.clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return pvcList.Items, nil
}

// ListJobs 列出 jobs
func (c *Client) ListJobs(ctx context.Context, namespace string) ([]interface{}, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	gvr := schema.GroupVersionResource{
		Group:    "batch",
		Version:  "v1",
		Resource: "jobs",
	}

	unstructuredList, err := c.dynClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list jobs: %w", err)
	}

	result := make([]interface{}, 0, len(unstructuredList.Items))
	for _, item := range unstructuredList.Items {
		result = append(result, item.Object)
	}
	return result, nil
}

// UpdateDeployment 更新 deployment
func (c *Client) UpdateDeployment(ctx context.Context, namespace, name string, deployment *appsv1.Deployment) error {
	if namespace == "" {
		namespace = c.namespace
	}
	_, err := c.clientset.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment: %w", err)
	}
	return nil
}

// CreateDeployment 创建 deployment
func (c *Client) CreateDeployment(ctx context.Context, namespace string, deployment *appsv1.Deployment) error {
	if namespace == "" {
		namespace = c.namespace
	}
	_, err := c.clientset.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}
	return nil
}

// CreateService 创建 service
func (c *Client) CreateService(ctx context.Context, namespace string, service *corev1.Service) error {
	if namespace == "" {
		namespace = c.namespace
	}
	_, err := c.clientset.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}
	return nil
}

// UpdateService 更新 service
func (c *Client) UpdateService(ctx context.Context, namespace string, service *corev1.Service) error {
	if namespace == "" {
		namespace = c.namespace
	}
	_, err := c.clientset.CoreV1().Services(namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update service: %w", err)
	}
	return nil
}

// DeleteService 删除 service
func (c *Client) DeleteService(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = c.namespace
	}
	return c.clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// CreateSecret 创建 secret
func (c *Client) CreateSecret(ctx context.Context, namespace string, secret *corev1.Secret) error {
	if namespace == "" {
		namespace = c.namespace
	}
	_, err := c.clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create secret: %w", err)
	}
	return nil
}

// UpdateSecret 更新 secret
func (c *Client) UpdateSecret(ctx context.Context, namespace string, secret *corev1.Secret) error {
	if namespace == "" {
		namespace = c.namespace
	}
	_, err := c.clientset.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update secret: %w", err)
	}
	return nil
}

// GetDeployment 获取 deployment
func (c *Client) GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
	if namespace == "" {
		namespace = c.namespace
	}
	return c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
}

// GetService 获取 service
func (c *Client) GetService(ctx context.Context, namespace, name string) (*corev1.Service, error) {
	if namespace == "" {
		namespace = c.namespace
	}
	return c.clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
}

// GetNode 获取 node
func (c *Client) GetNode(ctx context.Context, name string) (*corev1.Node, error) {
	return c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
}

// ListPersistentVolumes 列出 persistent volumes
func (c *Client) ListPersistentVolumes(ctx context.Context) ([]corev1.PersistentVolume, error) {
	pvList, err := c.clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return pvList.Items, nil
}

// WatchNodes 监听 nodes 变化
func (c *Client) WatchNodes(ctx context.Context, handler func(string, interface{})) error {
	watcher, err := c.clientset.CoreV1().Nodes().Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to watch nodes: %w", err)
	}

	fmt.Println("Watching nodes... (Press Ctrl+C to stop)")

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return nil
			}
			handler(string(event.Type), event.Object)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// WatchDeployments 监听 deployments 变化
func (c *Client) WatchDeployments(ctx context.Context, namespace string, handler func(string, interface{})) error {
	if namespace == "" {
		namespace = c.namespace
	}

	watcher, err := c.clientset.AppsV1().Deployments(namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to watch deployments: %w", err)
	}

	fmt.Println("Watching deployments... (Press Ctrl+C to stop)")

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return nil
			}
			handler(string(event.Type), event.Object)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// WatchServices 监听 services 变化
func (c *Client) WatchServices(ctx context.Context, namespace string, handler func(string, interface{})) error {
	if namespace == "" {
		namespace = c.namespace
	}

	watcher, err := c.clientset.CoreV1().Services(namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to watch services: %w", err)
	}

	fmt.Println("Watching services... (Press Ctrl+C to stop)")

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return nil
			}
			handler(string(event.Type), event.Object)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
