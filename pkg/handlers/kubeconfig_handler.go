package handlers

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zxh326/kite/pkg/auth"
	"github.com/zxh326/kite/pkg/cluster"
	"github.com/zxh326/kite/pkg/model"
	"github.com/zxh326/kite/pkg/rbac"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
)

// ProxyKubeconfigHandler returns the kubeconfig for clusters that the
// authenticated API-key user is allowed to proxy through kite-proxy.
//
// This endpoint is intentionally available only to API-key users so that
// kite-proxy can fetch credentials without a browser session.  The response
// must be treated as sensitive; kite-proxy MUST keep it in memory only.
//
// Query params:
//
//	cluster (optional) – restrict the response to a single named cluster.
func ProxyKubeconfigHandler(cm *cluster.ClusterManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := c.MustGet("user").(model.User)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// Only API-key users may call this endpoint.
		if _, device := c.Get(auth.ProxySessionKey); user.Provider != "api_key" && !device {
			c.JSON(http.StatusForbidden, gin.H{"error": "this endpoint is only available to API-key users"})
			return
		}

		clusterFilter := c.Query("cluster")

		// Build result: cluster name -> kubeconfig YAML string.
		type clusterKubeconfig struct {
			Name       string `json:"name"`
			Kubeconfig string `json:"kubeconfig"`
		}

		var results []clusterKubeconfig
		var noPermissionClusters []string
		var errorClusters []string

		clusters, _, _ := cm.GetAllClusters()
		for name, cs := range clusters {
			if clusterFilter != "" && name != clusterFilter {
				continue
			}
			// Check that user has proxy access for this cluster (namespace filtering
			// is kite-proxy's responsibility; see /proxy/namespaces for the allowed list).
			if !rbac.CanProxyCluster(user, name) {
				noPermissionClusters = append(noPermissionClusters, name)
				klog.V(2).Infof("ProxyKubeconfig: User %s has no proxy permission for cluster %s", user.Key(), name)
				continue
			}

			// First, try to get stored kubeconfig
			kc := cs.GetKubeconfig()
			if kc == "" {
				// For InCluster or clusters without stored config, generate from rest.Config
				if cs.K8sClient != nil && cs.K8sClient.Configuration != nil {
					var err error
					kc, err = generateKubeconfigFromRestConfig(name, cs.K8sClient.Configuration)
					if err != nil {
						klog.Warningf("ProxyKubeconfig: Failed to generate kubeconfig for cluster %s: %v", name, err)
						errorClusters = append(errorClusters, fmt.Sprintf("%s (generation failed)", name))
						continue
					}
					klog.V(2).Infof("ProxyKubeconfig: Generated kubeconfig from rest.Config for cluster %s", name)
				} else {
					klog.Warningf("ProxyKubeconfig: Cluster %s has no kubeconfig and no rest.Config available", name)
					errorClusters = append(errorClusters, fmt.Sprintf("%s (no config available)", name))
					continue
				}
			}

			results = append(results, clusterKubeconfig{
				Name:       name,
				Kubeconfig: kc,
			})
			klog.V(2).Infof("ProxyKubeconfig: Including cluster %s for user %s", name, user.Key())
		}

		if len(results) == 0 {
			klog.Warningf("ProxyKubeconfig: No clusters available for user %s. No permission: %v, Errors: %v",
				user.Key(), noPermissionClusters, errorClusters)
			c.JSON(http.StatusForbidden, gin.H{
				"error": "no clusters available for proxy or proxy not permitted",
				"details": gin.H{
					"no_permission": noPermissionClusters,
					"errors":        errorClusters,
				},
			})
			return
		}

		klog.V(1).Infof("ProxyKubeconfig: Returning %d cluster(s) for user %s", len(results), user.Key())
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"clusters": results})
	}
}

// ProxyNamespacesHandler returns the namespaces each cluster allows the
// authenticated API-key user to proxy through kite-proxy.
//
// The cluster to query can be supplied as:
//   - query param:  ?cluster=yunqiao
//   - JSON body:    {"cluster": "yunqiao"}
//
// If the role allows "*" namespaces, the handler fetches the live namespace list
// from the cluster so the caller always receives concrete names.
func ProxyNamespacesHandler(cm *cluster.ClusterManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := c.MustGet("user").(model.User)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if _, device := c.Get(auth.ProxySessionKey); user.Provider != "api_key" && !device {
			c.JSON(http.StatusForbidden, gin.H{"error": "this endpoint is only available to API-key users"})
			return
		}

		// Accept cluster filter from query param or JSON body.
		clusterFilter := c.Query("cluster")
		if clusterFilter == "" {
			var body struct {
				Cluster string `json:"cluster"`
			}
			_ = c.ShouldBindJSON(&body)
			clusterFilter = body.Cluster
		}

		type clusterNamespaces struct {
			Name       string   `json:"name"`
			Namespaces []string `json:"namespaces"`
		}

		var results []clusterNamespaces
		clusters, _, _ := cm.GetAllClusters()

		for name, cs := range clusters {
			if clusterFilter != "" && name != clusterFilter {
				continue
			}

			allowed := rbac.GetProxyNamespaces(user, name)
			if allowed == nil {
				// User has no proxy access for this cluster.
				continue
			}

			// If wildcard, expand to actual namespace list from the cluster.
			if len(allowed) > 0 {
				if cs.K8sClient != nil && cs.K8sClient.ClientSet != nil {
					ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
					nsList, err := cs.K8sClient.ClientSet.CoreV1().Namespaces().List(
						ctx, metav1.ListOptions{},
					)
					cancel()
					if err != nil {
						klog.Warningf("ProxyNamespaces: failed to list namespaces for cluster %s: %v", name, err)
						// Fall back to ["*"] rather than hard-failing.
					} else {
						expanded := make([]string, 0, len(nsList.Items))
						for _, ns := range nsList.Items {
							if rbac.CanProxy(user, name, ns.Name) {
								expanded = append(expanded, ns.Name)
							}
						}
						allowed = expanded
					}
				}
			}

			results = append(results, clusterNamespaces{
				Name:       name,
				Namespaces: allowed,
			})
			klog.V(2).Infof("ProxyNamespaces: cluster %s namespaces %v for user %s", name, allowed, user.Key())
		}

		if len(results) == 0 {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "no clusters available for proxy or proxy not permitted",
				"code":  "proxy_forbidden",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{"clusters": results})
	}
}

// generateKubeconfigFromRestConfig creates a kubeconfig YAML from a rest.Config.
// This is used for InCluster configurations or when no stored kubeconfig is available.
func generateKubeconfigFromRestConfig(clusterName string, restConfig *rest.Config) (string, error) {
	restConfig = rest.CopyConfig(restConfig)
	if err := rest.LoadTLSFiles(restConfig); err != nil {
		return "", fmt.Errorf("cannot inline cluster TLS credentials: %w", err)
	}
	if restConfig.BearerTokenFile != "" {
		token, err := os.ReadFile(restConfig.BearerTokenFile)
		if err != nil {
			return "", fmt.Errorf("cannot read cluster token: %w", err)
		}
		restConfig.BearerToken = strings.TrimSpace(string(token))
		clear(token)
		restConfig.BearerTokenFile = ""
	}
	config := clientcmdapi.NewConfig()

	// Create cluster entry
	cluster := &clientcmdapi.Cluster{
		Server:                   restConfig.Host,
		CertificateAuthorityData: restConfig.CAData,
		InsecureSkipTLSVerify:    restConfig.Insecure,
	}

	// If CAData is empty but CAFile is specified, read it
	if len(cluster.CertificateAuthorityData) == 0 && restConfig.CAFile != "" {
		cluster.CertificateAuthority = restConfig.CAFile
	}

	config.Clusters[clusterName] = cluster

	// Create auth info
	authInfo := &clientcmdapi.AuthInfo{
		ClientCertificateData: restConfig.CertData,
		ClientKeyData:         restConfig.KeyData,
		Token:                 restConfig.BearerToken,
		Username:              restConfig.Username,
		Password:              restConfig.Password,
	}

	// If CertData is empty but CertFile is specified, reference the file
	if len(authInfo.ClientCertificateData) == 0 && restConfig.CertFile != "" {
		authInfo.ClientCertificate = restConfig.CertFile
	}
	if len(authInfo.ClientKeyData) == 0 && restConfig.KeyFile != "" {
		authInfo.ClientKey = restConfig.KeyFile
	}

	// Handle token file for InCluster config
	if authInfo.Token == "" && restConfig.BearerTokenFile != "" {
		// For token files, we need to read the token since kite-proxy won't have access to the file
		// Note: This is a limitation - the token might expire
		klog.V(2).Infof("Cluster %s uses BearerTokenFile, token may expire", clusterName)
		authInfo.TokenFile = restConfig.BearerTokenFile
	}

	config.AuthInfos[clusterName] = authInfo

	// Create context
	context := &clientcmdapi.Context{
		Cluster:  clusterName,
		AuthInfo: clusterName,
	}
	config.Contexts[clusterName] = context
	config.CurrentContext = clusterName

	// Convert to YAML
	kubeconfigBytes, err := clientcmd.Write(*config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal kubeconfig: %w", err)
	}

	return string(kubeconfigBytes), nil
}
