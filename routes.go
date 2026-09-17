package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zxh326/kite/pkg/ai"
	"github.com/zxh326/kite/pkg/auth"
	"github.com/zxh326/kite/pkg/cluster"
	"github.com/zxh326/kite/pkg/handlers"
	"github.com/zxh326/kite/pkg/handlers/resources"
	"github.com/zxh326/kite/pkg/middleware"
	"github.com/zxh326/kite/pkg/rbac"
	"github.com/zxh326/kite/pkg/version"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

func setupAPIRouter(r *gin.RouterGroup, cm *cluster.ClusterManager) {
	authHandler := auth.NewAuthHandler()

	registerBaseRoutes(r, authHandler)
	registerAuthRoutes(r, authHandler)
	registerUserRoutes(r, authHandler, cm)
	registerAdminRoutes(r, authHandler, cm)
	registerProtectedRoutes(r, authHandler, cm)
}

func registerBaseRoutes(r *gin.RouterGroup, authHandler *auth.AuthHandler) {
	r.GET("/metrics", authHandler.RequireAuth(), gin.WrapH(promhttp.HandlerFor(prometheus.Gatherers{
		prometheus.DefaultGatherer,
		ctrlmetrics.Registry,
	}, promhttp.HandlerOpts{})))
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/api/v1/init_check", handlers.InitCheck)
	r.GET("/api/v1/version", version.GetVersion)
}

func registerAuthRoutes(r *gin.RouterGroup, authHandler *auth.AuthHandler) {
	authGroup := r.Group("/api/auth")
	authGroup.GET("/providers", authHandler.GetProviders)
	authGroup.POST("/login/password", middleware.LoginRateLimit(), authHandler.PasswordLogin)
	authGroup.POST("/login/ldap", middleware.LoginRateLimit(), authHandler.LDAPLogin)
	authGroup.GET("/login", authHandler.Login)
	authGroup.GET("/callback", authHandler.Callback)
	authGroup.POST("/logout", authHandler.Logout)
	authGroup.POST("/refresh", authHandler.RefreshToken)
	authGroup.GET("/user", authHandler.RequireAuth(), authHandler.GetUser)
	authGroup.POST("/proxy/token", middleware.LoginRateLimit(), auth.ProxyToken)
	devices := authGroup.Group("/proxy", authHandler.RequireAuth(), auth.RequireProxyBrowser)
	devices.POST("/authorize", auth.AuthorizeProxy)
	devices.GET("/sessions", auth.ListProxySessions)
	devices.DELETE("/sessions/:id", auth.RevokeProxySession)
}

func registerUserRoutes(r *gin.RouterGroup, authHandler *auth.AuthHandler, cm *cluster.ClusterManager) {
	userGroup := r.Group("/api/users")
	userGroup.POST("/sidebar_preference", authHandler.RequireAuth(), handlers.UpdateSidebarPreference)
	userGroup.POST("/default_cluster", authHandler.RequireAuth(), handlers.UpdateDefaultCluster)

	// Feishu card-action callback — no auth (called by Feishu servers)
	r.POST("/api/feishu/card-callback", handlers.HandleFeishuCardCallback)

	// Access-request endpoints for authenticated users
	arGroup := r.Group("/api/v1/access-requests")
	arGroup.Use(authHandler.RequireAuth())
	arGroup.GET("", handlers.ListMyAccessRequests)
	arGroup.POST("", middleware.ClusterMiddleware(cm), handlers.CreateAccessRequest)
	arGroup.PUT("/:id/withdraw", handlers.WithdrawAccessRequest)
	arGroup.POST("/:id/remind", handlers.RemindAccessRequest)

	// Approver list for the request dialog (auth required, no admin required)
	r.GET("/api/v1/feishu-approvers", authHandler.RequireAuth(), handlers.GetFeishuApprovers)
}

func registerAdminRoutes(r *gin.RouterGroup, authHandler *auth.AuthHandler, cm *cluster.ClusterManager) {
	adminAPI := r.Group("/api/v1/admin")
	adminAPI.POST("/users/create_super_user", handlers.CreateSuperUser)
	adminAPI.Use(authHandler.RequireAuth(), authHandler.RequireAdmin())

	adminAPI.POST("/clusters/import", cm.ImportClustersFromKubeconfig)
	adminAPI.GET("/audit-logs", handlers.ListAuditLogs)
	adminAPI.GET("/audit-logs/:id", handlers.GetAuditLogDetail)

	oauthProviderAPI := adminAPI.Group("/oauth-providers")
	oauthProviderAPI.GET("/", authHandler.ListOAuthProviders)
	oauthProviderAPI.POST("/", authHandler.CreateOAuthProvider)
	oauthProviderAPI.GET("/:id", authHandler.GetOAuthProvider)
	oauthProviderAPI.PUT("/:id", authHandler.UpdateOAuthProvider)
	oauthProviderAPI.DELETE("/:id", authHandler.DeleteOAuthProvider)

	ldapSettingAPI := adminAPI.Group("/ldap-setting")
	ldapSettingAPI.GET("/", authHandler.GetLDAPSetting)
	ldapSettingAPI.PUT("/", authHandler.UpdateLDAPSetting)

	clusterAPI := adminAPI.Group("/clusters")
	clusterAPI.GET("/", cm.GetClusterList)
	clusterAPI.POST("/", cm.CreateCluster)
	clusterAPI.PUT("/:id", cm.UpdateCluster)
	clusterAPI.DELETE("/:id", cm.DeleteCluster)

	rbacAPI := adminAPI.Group("/roles")
	rbacAPI.GET("/", rbac.ListRoles)
	rbacAPI.POST("/", rbac.CreateRole)
	rbacAPI.GET("/:id", rbac.GetRole)
	rbacAPI.PUT("/:id", rbac.UpdateRole)
	rbacAPI.DELETE("/:id", rbac.DeleteRole)
	rbacAPI.POST("/:id/assign", rbac.AssignRole)
	rbacAPI.DELETE("/:id/assign", rbac.UnassignRole)

	userAPI := adminAPI.Group("/users")
	userAPI.GET("/", handlers.ListUsers)
	userAPI.POST("/", handlers.CreatePasswordUser)
	userAPI.PUT("/:id", handlers.UpdateUser)
	userAPI.DELETE("/:id", handlers.DeleteUser)
	userAPI.POST("/:id/reset_password", handlers.ResetPassword)
	userAPI.POST("/:id/enable", handlers.SetUserEnabled)

	userGroupAPI := adminAPI.Group("/user-groups")
	userGroupAPI.GET("/", handlers.ListUserGroups)
	userGroupAPI.POST("/", handlers.CreateUserGroup)
	userGroupAPI.GET("/:id", handlers.GetUserGroup)
	userGroupAPI.PUT("/:id", handlers.UpdateUserGroup)
	userGroupAPI.DELETE("/:id", handlers.DeleteUserGroup)
	adminAPI.POST("/sidebar_preference/global", handlers.UpdateGlobalSidebarPreference)
	adminAPI.DELETE("/sidebar_preference/global", handlers.ClearGlobalSidebarPreference)

	apiKeyAPI := adminAPI.Group("/apikeys")
	apiKeyAPI.GET("/", handlers.ListAPIKeys)
	apiKeyAPI.POST("/", handlers.CreateAPIKey)
	apiKeyAPI.DELETE("/:id", handlers.DeleteAPIKey)

	generalSettingAPI := adminAPI.Group("/general-setting")
	generalSettingAPI.GET("/", ai.HandleGetGeneralSetting)
	generalSettingAPI.PUT("/", ai.HandleUpdateGeneralSetting)

	templateAPI := adminAPI.Group("/templates")
	templateAPI.POST("/", handlers.CreateTemplate)
	templateAPI.PUT("/:id", handlers.UpdateTemplate)
	templateAPI.DELETE("/:id", handlers.DeleteTemplate)

	// Access request admin endpoints
	accessRequestAdminAPI := adminAPI.Group("/access-requests")
	accessRequestAdminAPI.GET("/", handlers.ListAllAccessRequests)
	accessRequestAdminAPI.PUT("/:id/approve", handlers.ApproveAccess)
	accessRequestAdminAPI.PUT("/:id/revoke", handlers.RevokeAccess)

	// Feishu notification setting
	feishuSettingAPI := adminAPI.Group("/feishu-setting")
	feishuSettingAPI.GET("/", handlers.GetFeishuSetting)
	feishuSettingAPI.PUT("/", handlers.UpdateFeishuSetting)

	// Proxy kubeconfig endpoint – auth required, no admin required (API-key users with
	// proxy permission can call this to retrieve kubeconfigs for kite-proxy).
	proxyAPI := r.Group("/api/v1/proxy")
	proxyAPI.Use(authHandler.RequireProxyAuth())
	proxyAPI.GET("/session", auth.CurrentProxySession)
	proxyAPI.DELETE("/session", auth.CurrentProxySession)
	proxyAPI.GET("/kubeconfig", handlers.ProxyKubeconfigHandler(cm))
	proxyAPI.GET("/namespaces", handlers.ProxyNamespacesHandler(cm))
}

func registerProtectedRoutes(r *gin.RouterGroup, authHandler *auth.AuthHandler, cm *cluster.ClusterManager) {
	api := r.Group("/api/v1")
	api.GET("/clusters", authHandler.RequireAuth(), cm.GetClusters)
	api.Use(authHandler.RequireAuth(), middleware.ClusterMiddleware(cm))

	api.GET("/overview", handlers.GetOverview)
	api.GET("/gpu-overview", handlers.GetGPUOverview)

	promHandler := handlers.NewPromHandler()
	api.GET("/prometheus/resource-usage-history", promHandler.GetResourceUsageHistory)
	api.GET("/prometheus/pods/:namespace/:podName/metrics", promHandler.GetPodMetrics)

	logsHandler := handlers.NewLogsHandler()
	api.GET("/logs/:namespace/:podName", logsHandler.HandleLogs)
	api.GET("/logs/:namespace/:podName/ws", logsHandler.HandleLogsWebSocket)

	terminalHandler := handlers.NewTerminalHandler()
	api.GET("/terminal/:namespace/:podName/ws", terminalHandler.HandleTerminalWebSocket)

	nodeTerminalHandler := handlers.NewNodeTerminalHandler()
	api.GET("/node-terminal/:nodeName/ws", nodeTerminalHandler.HandleNodeTerminalWebSocket)

	kubectlTerminalHandler := handlers.NewKubectlTerminalHandler()
	api.GET("/kubectl-terminal/ws", kubectlTerminalHandler.HandleKubectlTerminalWebSocket)

	searchHandler := handlers.NewSearchHandler()
	api.GET("/search", searchHandler.GlobalSearch)

	resourceApplyHandler := handlers.NewResourceApplyHandler()
	api.POST("/resources/apply", resourceApplyHandler.ApplyResource)

	api.GET("/image/tags", handlers.GetImageTags)
	api.GET("/templates", handlers.ListTemplates)

	proxyHandler := handlers.NewProxyHandler()
	proxyHandler.RegisterRoutes(api)

	api.GET("/ai/status", ai.HandleAIStatus)
	api.POST("/ai/chat", ai.HandleChat)
	api.POST("/ai/execute/continue", ai.HandleExecuteContinue)
	api.POST("/ai/input/continue", ai.HandleInputContinue)
	api.DELETE("/ai/session/:id", ai.HandleDeleteConversationSession)

	api.Use(middleware.RBACMiddleware())
	resources.RegisterRoutes(api)
}
