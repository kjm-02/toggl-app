package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type GetWorksResponse struct {
	Works []Works `json:"works"`
	Sum   int     `json:"sum"`
}

func GetWorksAPI(repo WorkReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth0_id := c.GetString("auth0_id")
		reportDate := c.Query("report_date")

		if auth0_id == "" || reportDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "missing params",
			})
			return
		}

		works, sum, err := repo.GetWorks(auth0_id, reportDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"works": works,
			"sum":   sum,
		})
	}
}

// teams出力用にGroupby
func GetWorksSummaryAPI(repo WorkReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth0_id := c.GetString("auth0_id")
		report_date := c.Query("report_date")
		summaries, summay_err := repo.GetSummary(auth0_id, report_date)
		if summay_err != nil {
			c.JSON(500, gin.H{"error": summay_err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"summaries": summaries,
		})
	}
}

func CreateWorksAPI(repo WorkWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth0_id := c.GetString("auth0_id")

		var req reportRequestBody
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{
				"error":   err.Error(),
				"message": req,
			})
			return
		}

		err := repo.CreateWork(auth0_id, req)
		if err != nil {
			c.JSON(500, gin.H{"error": "Cannot create works"})
			log.Println(err.Error())
			return
		}

		c.JSON(200, gin.H{"status": "ok"})
	}
}

func EndWorksAPI(repo WorkWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth0_id := c.GetString("auth0_id")

		var req reportRequestBody
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{
				"error":   err.Error(),
				"message": req,
			})
			return
		}

		err := repo.EndWork(auth0_id, req)
		if err != nil {
			c.JSON(500, gin.H{"error": "Cannot create works"})
			log.Println(err.Error())
			return
		}

		c.JSON(200, gin.H{"status": "ok"})
	}
}

func UpdateWorksAPI(repo WorkWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth0_id := c.GetString("auth0_id")
		work_id := c.Param("id")

		var req Works
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{
				"error":   err.Error(),
				"message": req,
			})
			return
		}

		err := repo.UpdateWork(auth0_id, req, work_id)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			log.Println(err.Error())
			return
		}
		c.JSON(200, gin.H{"status": "ok"})
	}
}

func DeleteWorksAPI(repo WorkWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth0_id := c.GetString("auth0_id")
		work_id := c.Param("id")

		err := repo.DeleteWork(auth0_id, work_id)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			log.Println(err.Error())
			return
		}
		c.JSON(200, gin.H{"status": "ok"})
	}
}
