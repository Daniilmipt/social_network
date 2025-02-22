package main

import (
	"chatroom/config"
	"chatroom/pkg"
	"chatroom/pkg/models"
	chrouter "chatroom/router"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	cfg := config.ParseConfig()

	logger, f := pkg.SetupLogger()
	defer f.Close()

	msgCh := make(chan models.Messages, 10)
	defer close(msgCh)

	router := gin.Default()
	router.Use(corsMiddleware())

	chatHandler := chrouter.NewChatHandler(logger, msgCh, cfg.Backend)

	router.GET("/", chatHandler.GetAuthView)

	authorized := router.Group("/")
	authorized.Use(checkNickMiddleware())
	authorized.GET("/room", chatHandler.GetRoomView)
	authorized.GET("/rooms-list", chatHandler.GetRoomsListView)

	internalApi := authorized.Group("/api")
	internalApi.GET("/rooms-last-message", chatHandler.GetRoomsLastMessage)
	internalApi.GET("/messages", chatHandler.GetMessagesFile)
	internalApi.POST("/send-message", chatHandler.SendMessage)
	internalApi.DELETE("/clear", chatHandler.ClearStdin)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%s", cfg.Frontend.Port),
		Handler: router,
	}
	go func() {
		logger.Info(fmt.Sprintf("Server started at http://localhost:%s", cfg.Frontend.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	if err := os.Mkdir("./messages", os.ModePerm); err != nil {
		logger.Info("can not create messages dis", zap.Error(err))
	}

	<-stop
	logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("failed to shutdown server", zap.Error(err))
		}
		logger.Info("Server exited gracefully")
	}()
	wg.Wait()

}

func checkNickMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		nick, err := c.Cookie("user")
		if err != nil || nick == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "You must have a 'user' cookie to access this endpoint.",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// corsMiddleware adds CORS headers to the response
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, X-Auth-Token")
		}

		// Handle preflight requests
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}
