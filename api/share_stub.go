package api

import (
	"net/http"
	"strconv"
	"strings"

	"sublink/models"
	"sublink/utils"

	"github.com/gin-gonic/gin"
)

// ShareStubReq is the create/update payload for a stub (Заглушка).
type ShareStubReq struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	CaseKey string `json:"case_key"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Enabled *bool  `json:"enabled"`
}

// ShareSubscriptionStubReq replaces per-link stub overrides.
type ShareSubscriptionStubReq struct {
	ShareID int            `json:"share_id" binding:"required"`
	Stubs   map[string]int `json:"stubs"`
}

type shareStubCaseInfo struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// shareStubCaseLabels maps a case key to a human-readable label.
func shareStubCaseLabels() map[string]string {
	return map[string]string{
		models.ShareStubCaseDeviceLimit:      "Device limit reached",
		models.ShareStubCaseDevicePending:    "Device approval required",
		models.ShareStubCaseDeviceBlocked:    "Device blocked",
		models.ShareStubCaseQuotaExhausted:   "Traffic limit exhausted",
		models.ShareStubCaseExpired:          "Link expired",
		models.ShareStubCaseGroupDisabled:    "Group disabled",
		models.ShareStubCaseUnsupportedApp:   "Unsupported app",
		models.ShareStubCaseTemplateDisabled: "Template not enabled",
		models.ShareStubCaseInvalidToken:     "Invalid link",
	}
}

// ShareStubList returns the shared stub library and the known cases.
func ShareStubList(c *gin.Context) {
	labels := shareStubCaseLabels()
	cases := make([]shareStubCaseInfo, 0, len(models.ShareStubCaseKeys))
	for _, key := range models.ShareStubCaseKeys {
		cases = append(cases, shareStubCaseInfo{Key: key, Label: labels[key]})
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"items": models.ListShareStubs(),
			"cases": cases,
		},
	})
}

// ShareStubAdd creates a stub in the shared library.
func ShareStubAdd(c *gin.Context) {
	var req ShareStubReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}
	stub := &models.ShareStub{
		Name:    strings.TrimSpace(req.Name),
		CaseKey: strings.TrimSpace(req.CaseKey),
		Title:   strings.TrimSpace(req.Title),
		Content: req.Content,
		Enabled: true,
	}
	if req.Enabled != nil {
		stub.Enabled = *req.Enabled
	}
	if err := stub.Add(); err != nil {
		utils.Error("Failed to create share stub: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Created successfully", "data": stub})
}

// ShareStubUpdate changes editable stub fields.
func ShareStubUpdate(c *gin.Context) {
	var req ShareStubReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}
	if req.ID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid stub ID"})
		return
	}
	stub := &models.ShareStub{ID: req.ID}
	if err := stub.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Stub not found"})
		return
	}
	if req.CaseKey != "" {
		stub.CaseKey = strings.TrimSpace(req.CaseKey)
	}
	stub.Name = strings.TrimSpace(req.Name)
	stub.Title = strings.TrimSpace(req.Title)
	stub.Content = req.Content
	if req.Enabled != nil {
		stub.Enabled = *req.Enabled
	}
	if err := stub.Update(); err != nil {
		utils.Error("Failed to update share stub: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Updated successfully", "data": stub})
}

// ShareStubDelete removes a stub and its mappings.
func ShareStubDelete(c *gin.Context) {
	id, err := strconv.Atoi(c.Query("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid stub ID"})
		return
	}
	stub := &models.ShareStub{ID: id}
	if err := stub.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Stub not found"})
		return
	}
	if err := stub.Delete(); err != nil {
		utils.Error("Failed to delete share stub: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Delete failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Deleted successfully"})
}

// ShareSubscriptionStubsGet returns the per-link stub overrides.
func ShareSubscriptionStubsGet(c *gin.Context) {
	id, err := strconv.Atoi(c.Query("shareId"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid share ID"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": models.GetShareSubscriptionStubMap(id)})
}

// ShareSubscriptionStubsUpdate replaces the per-link stub overrides.
func ShareSubscriptionStubsUpdate(c *gin.Context) {
	var req ShareSubscriptionStubReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}
	share := &models.SubscriptionShare{ID: req.ShareID}
	if err := share.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Share not found"})
		return
	}
	if err := models.SetShareSubscriptionStubs(req.ShareID, req.Stubs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Updated successfully"})
}
