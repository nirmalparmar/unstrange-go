package response

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type errorBody struct {
	Error string `json:"error"`
}

func Error(c *gin.Context, status int, msg string) {
	c.JSON(status, errorBody{Error: msg})
}

// ListResponse wraps paginated results.
type ListResponse struct {
	Items   any   `json:"items"`
	Total   int64 `json:"total"`
	HasMore bool  `json:"has_more"`
}

func List(c *gin.Context, items any, total int64, limit, offset int) {
	c.JSON(http.StatusOK, ListResponse{
		Items:   items,
		Total:   total,
		HasMore: int64(offset+limit) < total,
	})
}

// Pagination extracts limit/offset from query params with sensible defaults.
func Pagination(c *gin.Context) (limit, offset int) {
	limit = 20
	offset = 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}
