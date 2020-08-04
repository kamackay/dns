package server

import (
	"fmt"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	jsoniter "github.com/json-iterator/go"
	"gitlab.com/kamackay/dns/util"
	"net/http"
	"sort"
	"strings"
)

func (this *Server) startRest() {
	go func() {
		// Instantiate a new router
		gin.SetMode(gin.ReleaseMode)
		engine := gin.Default()
		engine.Use(gzip.Gzip(gzip.BestCompression))
		engine.Use(cors.Default())
		//engine.Use(logger.SetLogger())
		engine.GET("/", func(c *gin.Context) {
			this.stats.Domains = make([]*Domain, 0)
			this.domains.Range(func(key, value interface{}) bool {
				running := util.PrintTimeDiff(this.stats.Started)
				this.stats.Running = &running
				if key != nil && value != nil {
					this.stats.Domains = append(this.stats.Domains, value.(*Domain))
				}
				return true
			})
			sort.SliceStable(this.stats.Domains, func(i, j int) bool {
				return this.stats.Domains[i].Requests > this.stats.Domains[j].Requests
			})
			jsonData, err := jsoniter.MarshalIndent(this.stats, "", "  ")
			if err != nil {
				c.JSON(http.StatusOK, this.stats)
				return
			}
			engine.LoadHTMLGlob("templates/*")
			c.HTML(http.StatusOK, "json.tmpl", &gin.H{
				"json": strings.TrimSpace(string(jsonData)),
			})
		})

		if err := engine.Run(":9999"); err != nil {
			panic(err)
		} else {
			fmt.Printf("Successfully Started Server")
		}
	}()
}
