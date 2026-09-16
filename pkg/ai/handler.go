package ai

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zxh326/kite/pkg/cluster"
	"github.com/zxh326/kite/pkg/model"
)

// HandleAIStatus returns whether AI features are enabled.
func HandleAIStatus(c *gin.Context) {
	cfg, err := LoadRuntimeConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to load AI config: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled":  cfg.Enabled,
		"provider": cfg.Provider,
		"model":    cfg.Model,
	})
}

// HandleChat handles the SSE streaming chat endpoint.
func HandleChat(c *gin.Context) {
	cfg, err := LoadRuntimeConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to load AI config: %v", err)})
		return
	}
	if !cfg.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "AI is not enabled"})
		return
	}

	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}
	req.Language = detectRequestLanguage(req.Language, c.GetHeader("Accept-Language"))

	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No messages provided"})
		return
	}

	clientSet, ok := getClusterClientSet(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No cluster selected"})
		return
	}

	agent, err := NewAgent(clientSet, cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to create AI agent: %v", err)})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	agent.sessionID = strings.TrimSpace(req.SessionID)

	// Send SSE keepalive comments periodically to prevent proxy/load-balancer timeouts.
	// This is the root cause of the "network error" when AI takes a long time to respond.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-c.Request.Context().Done():
				return
			case <-ticker.C:
				// SSE comment — kept alive without triggering an event on the client
				_, _ = fmt.Fprint(c.Writer, ": keepalive\n\n")
				c.Writer.Flush()
			}
		}
	}()

	sendEvent := func(event SSEEvent) {
		// Don't write if client already disconnected
		select {
		case <-c.Request.Context().Done():
			return
		default:
		}
		data := MarshalSSEEvent(event)
		_, _ = fmt.Fprint(c.Writer, data)
		c.Writer.Flush()
	}

	agent.ProcessChat(c, &req, sendEvent)

	close(done)
	sendEvent(SSEEvent{Event: "done", Data: map[string]string{}})
}

type ContinueRequest struct {
	SessionID string `json:"sessionId"`
}

type ContinueInputRequest struct {
	SessionID string                 `json:"sessionId"`
	Values    map[string]interface{} `json:"values"`
}

// HandleExecuteContinue resumes a pending AI action after user confirmation.
func HandleExecuteContinue(c *gin.Context) {
	cfg, err := LoadRuntimeConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to load AI config: %v", err)})
		return
	}
	if !cfg.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "AI is not enabled"})
		return
	}

	var req ContinueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId is required"})
		return
	}

	clientSet, ok := getClusterClientSet(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No cluster selected"})
		return
	}

	agent, err := NewAgent(clientSet, cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to create AI agent: %v", err)})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sendEvent := func(event SSEEvent) {
		data := MarshalSSEEvent(event)
		_, _ = fmt.Fprint(c.Writer, data)
		c.Writer.Flush()
	}

	if err := agent.ContinuePendingAction(c, req.SessionID, sendEvent); err != nil {
		sendEvent(SSEEvent{Event: "error", Data: map[string]string{"message": err.Error()}})
	}

	sendEvent(SSEEvent{Event: "done", Data: map[string]string{}})
}

func HandleInputContinue(c *gin.Context) {
	cfg, err := LoadRuntimeConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to load AI config: %v", err)})
		return
	}
	if !cfg.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "AI is not enabled"})
		return
	}

	var req ContinueInputRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}
	if strings.TrimSpace(req.SessionID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sessionId is required"})
		return
	}

	clientSet, ok := getClusterClientSet(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No cluster selected"})
		return
	}

	agent, err := NewAgent(clientSet, cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to create AI agent: %v", err)})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	sendEvent := func(event SSEEvent) {
		data := MarshalSSEEvent(event)
		_, _ = fmt.Fprint(c.Writer, data)
		c.Writer.Flush()
	}

	if err := agent.ContinuePendingInput(c, req.SessionID, req.Values, sendEvent); err != nil {
		sendEvent(SSEEvent{Event: "error", Data: map[string]string{"message": err.Error()}})
	}

	sendEvent(SSEEvent{Event: "done", Data: map[string]string{}})
}

func HandleGetGeneralSetting(c *gin.Context) {
	setting, err := model.GetGeneralSetting()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to load general setting: %v", err)})
		return
	}
	hasAIAPIKey := strings.TrimSpace(string(setting.AIAPIKey)) != ""
	c.JSON(http.StatusOK, gin.H{
		"aiAgentEnabled":        setting.AIAgentEnabled,
		"aiProvider":            setting.AIProvider,
		"aiModel":               setting.AIModel,
		"aiApiKey":              "",
		"aiApiKeyConfigured":    hasAIAPIKey,
		"aiBaseUrl":             setting.AIBaseURL,
		"aiMaxTokens":           setting.AIMaxTokens,
		"aiReasoningEffort":     setting.AIReasoningEffort,
		"kubectlEnabled":        setting.KubectlEnabled,
		"kubectlImage":          setting.KubectlImage,
		"nodeTerminalImage":     setting.NodeTerminalImage,
		"enableVersionCheck":    setting.EnableVersionCheck,
		"passwordLoginDisabled": setting.PasswordLoginDisabled,
	})
}

type UpdateGeneralSettingRequest struct {
	AIAgentEnabled        bool    `json:"aiAgentEnabled"`
	AIProvider            string  `json:"aiProvider"`
	AIModel               string  `json:"aiModel"`
	AIAPIKey              *string `json:"aiApiKey"`
	AIBaseURL             string  `json:"aiBaseUrl"`
	AIMaxTokens           int     `json:"aiMaxTokens"`
	AIReasoningEffort     string  `json:"aiReasoningEffort"`
	KubectlEnabled        bool    `json:"kubectlEnabled"`
	KubectlImage          string  `json:"kubectlImage"`
	NodeTerminalImage     string  `json:"nodeTerminalImage"`
	EnableVersionCheck    bool    `json:"enableVersionCheck"`
	PasswordLoginDisabled *bool   `json:"passwordLoginDisabled"`
}

func HandleUpdateGeneralSetting(c *gin.Context) {
	var req UpdateGeneralSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}
	currentSetting, err := model.GetGeneralSetting()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to load general setting: %v", err)})
		return
	}

	aiProvider := strings.ToLower(strings.TrimSpace(req.AIProvider))
	if aiProvider == "" {
		aiProvider = currentSetting.AIProvider
	}
	if !model.IsGeneralAIProviderSupported(aiProvider) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported aiProvider"})
		return
	}
	aiProvider = normalizeProvider(aiProvider)

	aiModel := strings.TrimSpace(req.AIModel)
	if aiModel == "" {
		aiModel = model.DefaultGeneralAIModelByProvider(aiProvider)
	}
	aiReasoningEffort := strings.ToLower(strings.TrimSpace(req.AIReasoningEffort))
	if aiReasoningEffort == "" {
		aiReasoningEffort = currentSetting.AIReasoningEffort
	}
	if !model.IsGeneralAIReasoningEffortSupported(aiReasoningEffort) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported aiReasoningEffort"})
		return
	}
	aiAPIKey := strings.TrimSpace(string(currentSetting.AIAPIKey))
	shouldUpdateAIAPIKey := false
	if req.AIAPIKey != nil {
		incomingKey := strings.TrimSpace(*req.AIAPIKey)
		if incomingKey != "" {
			aiAPIKey = incomingKey
			shouldUpdateAIAPIKey = true
		}
	}
	if req.AIAgentEnabled && aiAPIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "aiApiKey is required when aiAgentEnabled is true"})
		return
	}

	kubectlImage := strings.TrimSpace(req.KubectlImage)
	if req.KubectlEnabled && strings.TrimSpace(req.KubectlImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "kubectlImage is required when kubectlEnabled is true"})
		return
	}
	if kubectlImage == "" {
		kubectlImage = model.DefaultGeneralKubectlImage
	}
	nodeTerminalImage := strings.TrimSpace(req.NodeTerminalImage)
	if nodeTerminalImage == "" {
		nodeTerminalImage = strings.TrimSpace(currentSetting.NodeTerminalImage)
	}
	if nodeTerminalImage == "" {
		nodeTerminalImage = model.DefaultGeneralNodeTerminalImageValue()
	}

	aiMaxTokens := req.AIMaxTokens
	if aiMaxTokens <= 0 {
		aiMaxTokens = 4096
	}

	updates := map[string]interface{}{
		"ai_agent_enabled":     req.AIAgentEnabled,
		"ai_provider":          aiProvider,
		"ai_model":             aiModel,
		"ai_base_url":          strings.TrimSpace(req.AIBaseURL),
		"ai_max_tokens":        aiMaxTokens,
		"ai_reasoning_effort":  aiReasoningEffort,
		"kubectl_enabled":      req.KubectlEnabled,
		"kubectl_image":        kubectlImage,
		"node_terminal_image":  nodeTerminalImage,
		"enable_version_check": req.EnableVersionCheck,
	}
	if req.PasswordLoginDisabled != nil {
		updates["password_login_disabled"] = *req.PasswordLoginDisabled
	}
	if shouldUpdateAIAPIKey {
		updates["ai_api_key"] = model.SecretString(aiAPIKey)
	}

	updated, err := model.UpdateGeneralSetting(updates)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to update general setting: %v", err)})
		return
	}

	hasAIAPIKey := strings.TrimSpace(string(updated.AIAPIKey)) != ""
	c.JSON(http.StatusOK, gin.H{
		"aiAgentEnabled":        updated.AIAgentEnabled,
		"aiProvider":            updated.AIProvider,
		"aiModel":               updated.AIModel,
		"aiApiKey":              "",
		"aiApiKeyConfigured":    hasAIAPIKey,
		"aiBaseUrl":             updated.AIBaseURL,
		"aiMaxTokens":           updated.AIMaxTokens,
		"aiReasoningEffort":     updated.AIReasoningEffort,
		"kubectlEnabled":        updated.KubectlEnabled,
		"kubectlImage":          updated.KubectlImage,
		"nodeTerminalImage":     updated.NodeTerminalImage,
		"enableVersionCheck":    updated.EnableVersionCheck,
		"passwordLoginDisabled": updated.PasswordLoginDisabled,
	})
}

func getClusterClientSet(c *gin.Context) (*cluster.ClientSet, bool) {
	cs, exists := c.Get("cluster")
	if !exists {
		return nil, false
	}
	clientSet, ok := cs.(*cluster.ClientSet)
	return clientSet, ok
}

// HandleDeleteConversationSession removes a conversation session from the server-side store.
func HandleDeleteConversationSession(c *gin.Context) {
	sessionID := c.Param("id")
	clientSet, ok := getClusterClientSet(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No cluster selected"})
		return
	}
	agentConversationStore.deleteForCluster(sessionID, clientSet.Name)
	c.Status(http.StatusNoContent)
}
