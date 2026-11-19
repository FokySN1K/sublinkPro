package api

import (
	"net/http"
	"strconv"
	"strings"

	"sublink/models"
	"sublink/utils"

	"github.com/gin-gonic/gin"
)

// shareClientItem is one client row with the number of subscription cards it
// owns. Unassigned cards are not a row here; the UI renders them separately.
type shareClientItem struct {
	models.ShareClient
	ShareCount int `json:"share_count"`
}

// ShareClientList returns every client with its card counter.
//
//	GET /api/v1/shares/clients?keyword=
func ShareClientList(c *gin.Context) {
	clients, err := models.SearchShareClients(c.Query("keyword"))
	if err != nil {
		utils.Error("Failed to list share clients: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to load clients"})
		return
	}
	counts, countErr := models.CountShareCardsByClient()
	if countErr != nil {
		utils.Warn("Failed to count share cards by client: %v", countErr)
		counts = map[int]int{}
	}
	items := make([]shareClientItem, 0, len(clients))
	for _, client := range clients {
		items = append(items, shareClientItem{ShareClient: client, ShareCount: counts[client.ID]})
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"items": items,
			"total": len(items),
		},
	})
}

// ShareClientAdd creates a client.
//
//	POST /api/v1/shares/clients/add {"name": "...", "note": "...", "enabled": true}
func ShareClientAdd(c *gin.Context) {
	var client models.ShareClient
	if err := c.ShouldBindJSON(&client); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}
	client.ID = 0
	client.Name = strings.TrimSpace(client.Name)
	if client.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Client name is required"})
		return
	}
	if err := client.Add(); err != nil {
		utils.Error("Failed to add share client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Created successfully", "data": client})
}

// ShareClientUpdate renames or re-enables a client.
//
//	POST /api/v1/shares/clients/update {"id": 1, "name": "...", "note": "...", "enabled": true}
func ShareClientUpdate(c *gin.Context) {
	var req struct {
		ID      int    `json:"id" binding:"required"`
		Name    string `json:"name"`
		Note    string `json:"note"`
		Enabled *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}
	client, err := models.GetShareClientByID(req.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Client not found"})
		return
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		client.Name = name
	}
	client.Note = strings.TrimSpace(req.Note)
	if req.Enabled != nil {
		client.Enabled = *req.Enabled
	}
	if err := client.Update(); err != nil {
		utils.Error("Failed to update share client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Updated successfully", "data": client})
}

// ShareClientDelete removes a client; its cards become unassigned.
//
//	DELETE /api/v1/shares/clients/delete?id=1
func ShareClientDelete(c *gin.Context) {
	id, err := strconv.Atoi(c.Query("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid client ID"})
		return
	}
	client, err := models.GetShareClientByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Client not found"})
		return
	}
	if err := client.Delete(); err != nil {
		utils.Error("Failed to delete share client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Delete failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Deleted successfully"})
}

// ShareClientAssign binds or unbinds subscription cards.
//
//	POST /api/v1/shares/clients/assign {"share_ids": [1,2], "client_id": 3}
//	POST /api/v1/shares/clients/assign {"share_ids": [1,2], "client_id": null}
func ShareClientAssign(c *gin.Context) {
	var req struct {
		ShareIDs []int `json:"share_ids"`
		ShareID  int   `json:"share_id"`
		ClientID *int  `json:"client_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}
	shareIDs := req.ShareIDs
	if len(shareIDs) == 0 && req.ShareID > 0 {
		shareIDs = []int{req.ShareID}
	}
	if len(shareIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "share_ids is required"})
		return
	}
	if err := models.AssignShareClients(shareIDs, req.ClientID); err != nil {
		utils.Error("Failed to assign share clients: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Updated successfully"})
}
