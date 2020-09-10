package server

import (
	"fmt"
	"github.com/avct/uasurfer"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/copier"
	jsoniter "github.com/json-iterator/go"
	"gitlab.com/kamackay/dns/util"
	"net/http"
	"sort"
	"strings"
)

func (this *Server) startRest(flush func() error) {
	go func() {
		// Instantiate a new router
		gin.SetMode(gin.ReleaseMode)
		engine := gin.Default()
		engine.Use(gzip.Gzip(gzip.BestCompression))
		engine.Use(cors.Default())

		engine.GET("/", func(ctx *gin.Context) {
			send := func(json interface{}) {
				ua := uasurfer.Parse(ctx.Request.UserAgent())
				if ua.IsBot() {
					ctx.String(http.StatusNotFound, "Not Found")
					return
				}
				isBrowser := ua.Browser.Name == uasurfer.BrowserChrome ||
					ua.Browser.Name == uasurfer.BrowserFirefox ||
					ua.Browser.Name == uasurfer.BrowserAndroid ||
					ua.Browser.Name == uasurfer.BrowserSafari
				jsonData, err := jsoniter.MarshalIndent(json, "", "  ")
				if err != nil || !isBrowser {
					ctx.JSON(http.StatusOK, json)
					return
				}
				ctx.HTML(http.StatusOK, "json.tmpl", &gin.H{
					"json": strings.TrimSpace(string(jsonData)),
				})
			}
			var stats Stats
			err := copier.Copy(&stats, &this.stats)
			if err != nil {
				// shrug
				fmt.Printf("%+v", err)
				send(gin.H{})
				return
			}
			if ctx.Query("metrics") != "true" {
				stats.Metrics = make([]Metric, 0)
			}
			stats.Domains = make([]*Domain, 0)
			this.domains.Range(func(key, value interface{}) bool {
				running := util.PrintTimeDiff(stats.Started)
				stats.Running = &running
				if key != nil && value != nil {
					stats.Domains = append(stats.Domains, value.(*Domain))
				}
				return true
			})
			sort.SliceStable(stats.Domains, func(i, j int) bool {
				return stats.Domains[i].Requests > stats.Domains[j].Requests
			})
			engine.LoadHTMLGlob("templates/*")
			send(stats)
		})

		engine.POST("/flush", func(ctx *gin.Context) {
			err := flush()
			if err != nil {
				ctx.String(http.StatusInternalServerError, "Error Clearing DNS Cache\n")
			} else {
				ctx.String(http.StatusOK, "Flushed!\n")
			}
		})

		if err := engine.Run(":9999"); err != nil {
			panic(err)
		} else {
			fmt.Printf("Successfully Started Server")
		}
	}()
}
