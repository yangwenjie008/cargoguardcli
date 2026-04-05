package k8s

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Client kubectl 客户端封装
type Client struct {
	clientset  *kubernetes.Clientset
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

// NewClient 创建 kubectl 客户端
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

	c.clientset = clientset
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

// PodWatchHandler Pod 监控回调
type PodWatchHandler func(event PodWatchEvent)

// PodWatchEvent Pod 监控事件
type PodWatchEvent struct {
	Type   string
	Pod    *PodInfo
	Object interface{}
}

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

// GetPodLogs 获取 pod 日志
func (c *Client) GetPodLogs(ctx context.Context, namespace, name, container string, tailLines int64) (string, error) {
	if namespace == "" {
		namespace = c.namespace
	}

	kubeArgs := []string{"logs", "-n", namespace, name}
	if container != "" {
		kubeArgs = append(kubeArgs, "-c", container)
	}
	if tailLines > 0 {
		kubeArgs = append(kubeArgs, fmt.Sprintf("--tail=%d", tailLines))
	}

	cmd := exec.CommandContext(ctx, "kubectl", kubeArgs...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get pod logs: %w", err)
	}
	return string(output), nil
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
		result = append(result, DeploymentInfo{
			Name:              dep.Name,
			Namespace:         dep.Namespace,
			Replicas:          int(*dep.Spec.Replicas),
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

// RestartDeployment 重启 deployment
func (c *Client) RestartDeployment(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = c.namespace
	}

	// 使用 rollout restart
	cmd := exec.CommandContext(ctx, "kubectl", "rollout", "restart", "deployment", name, "-n", namespace)
	return cmd.Run()
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
			Name:       node.Name,
			Status:     formatNodeStatus(&node),
			Roles:      getNodeRoles(&node),
			Age:        time.Since(node.CreationTimestamp.Time),
			Labels:     node.Labels,
			Allocatable: node.Status.Allocatable,
		})
	}
	return result, nil
}

// formatNodeStatus 格式化节点状态
func formatNodeStatus(node *corev1.Node) string {
	conditions := node.Status.Conditions
	for _, c := range conditions {
		if c.Type == corev1.NodeReady {
			if c.Status == corev1.ConditionTrue {
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

// ==================== YAML Operations ====================

// Apply 应用 YAML manifest
func (c *Client) Apply(ctx context.Context, manifest string) error {
	kubeArgs := []string{"apply", "-f", "-"}
	cmd := exec.CommandContext(ctx, "kubectl", kubeArgs...)
	cmd.Stdin = strings.NewReader(manifest)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apply failed: %s, %w", string(output), err)
	}
	return nil
}

// ApplyFile 应用 YAML 文件
func (c *Client) ApplyFile(ctx context.Context, filename string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", filename)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apply failed: %s, %w", string(output), err)
	}
	return nil
}

// Delete 删除资源
func (c *Client) Delete(ctx context.Context, resourceType, name, namespace string) error {
	kubeArgs := []string{"delete", resourceType, name}
	if namespace != "" {
		kubeArgs = append(kubeArgs, "-n", namespace)
	}

	cmd := exec.CommandContext(ctx, "kubectl", kubeArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete failed: %s, %w", string(output), err)
	}
	return nil
}

// Get 获取资源 YAML
func (c *Client) Get(ctx context.Context, resourceType, name, namespace string) (string, error) {
	kubeArgs := []string{"get", "-o", "yaml", resourceType, name}
	if namespace != "" {
		kubeArgs = append(kubeArgs, "-n", namespace)
	}

	cmd := exec.CommandContext(ctx, "kubectl", kubeArgs...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get failed: %s, %w", string(output), err)
	}
	return string(output), nil
}

// Describe 获取资源详情
func (c *Client) Describe(ctx context.Context, resourceType, name, namespace string) (string, error) {
	kubeArgs := []string{"describe", resourceType, name}
	if namespace != "" {
		kubeArgs = append(kubeArgs, "-n", namespace)
	}

	cmd := exec.CommandContext(ctx, "kubectl", kubeArgs...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("describe failed: %s, %w", string(output), err)
	}
	return string(output), nil
}

// Exec 在 Pod 中执行命令
func (c *Client) Exec(ctx context.Context, namespace, pod, container string, command []string) (string, error) {
	kubeArgs := []string{"exec", "-n", namespace, pod}
	if container != "" {
		kubeArgs = append(kubeArgs, "-c", container)
	}
	kubeArgs = append(kubeArgs, "--")
	kubeArgs = append(kubeArgs, command...)

	cmd := exec.CommandContext(ctx, "kubectl", kubeArgs...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("exec failed: %s, %w", string(output), err)
	}
	return string(output), nil
}

// PortForward 端口转发
func (c *Client) PortForward(ctx context.Context, namespace, pod string, localPort, remotePort int) error {
	kubeArgs := []string{"port-forward", "-n", namespace, pod, fmt.Sprintf("%d:%d", localPort, remotePort)}

	cmd := exec.CommandContext(ctx, "kubectl", kubeArgs...)
	return cmd.Run()
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
		"version":   version.GitVersion,
		"platform":  version.Platform,
		"context":   c.context,
		"namespace": c.namespace,
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
