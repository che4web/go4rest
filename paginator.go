package go4rest

import (
	"fmt"
	"strconv"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)
type PaginatedResponse struct {
	Results       interface{}      `json:"results"`
	Page          int              `json:"page"`
	TotalPages    int              `json:"total_pages"`
	Count         int              `json:"count"`
}

type Pagination struct {
	Page    int
	PerPage int
    TotalPages int
    Count int
}
func NewPaginator(ctx *gin.Context) Pagination{
    page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}

	perPage, _ := strconv.Atoi(ctx.DefaultQuery("per_page", "30"))
	switch {
	    case perPage > 100:
		    perPage = 100
	    case perPage <= 0:
		    perPage = 30
	}

    return Pagination{
        Page:page,
        PerPage:perPage,
    }

}
func(p *Pagination) PaginatedQueryset(query *gorm.DB) *gorm.DB {


	offset := (p.Page - 1) * p.PerPage
    var count int64
    query.Count(&count)
    fmt.Printf("%+v\n", count)
    p.Count = int(count)
    p.TotalPages = (p.Count+p.PerPage-1)/p.PerPage

    return query.Offset(offset).Limit(p.PerPage)
}
func(p *Pagination) GetResponse(data interface{}) PaginatedResponse{
    return PaginatedResponse{
        Results:data,
        Page:p.Page,
        TotalPages:p.TotalPages,
        Count:p.Count,

    }

}
