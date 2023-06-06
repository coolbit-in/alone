package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/coolbit-in/alone/openai"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var Backend openai.GptBackend

//	@title			Phantom Horse API
//	@version		v1.0
//	@description	This is a sample server celler server.

// create http api server use gin, and use gogpt as backend

// GetConversation doc
//
//	@Router			/conversations/{conversation_id} [get]
//	@Summary		Get conversation
//	@Description	Get conversation
//	@Tags			conversation
//	@Accept			json
//	@Produce		json
//	@Param			id	path		int	true	"Conversation ID"
//	@Success		200	{object}	openai.Conversation
//	@Failure		400	{object}	error
func GetConversation(c *gin.Context) {
	// bind path param: conversation_id
	convID := c.Param("conversation_id")
	if convID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversation_id is required"})
		return
	}
	//convert string to uint
	convIDUint, err := strconv.ParseUint(convID, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversation_id is invalid"})
		return
	}
	conv, err := Backend.GetConversation(uint(convIDUint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, conv)
}

// ListConversations doc
//
//	@Router			/conversations [get]
//	@Summary		List conversations
//	@Description	List conversations
//	@Tags			conversation
//	@Accept			json
//	@Produce		json
//	@Success		200	{array}		openai.Conversation
//	@Failure		500	{object}	string
//	@Failure		400	{object}	string
func ListConversations(c *gin.Context) {
	// call backend
	convs, err := Backend.ListConversations(nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, convs)
}

// AddConversation doc
//
//	@Router			/conversations [post]
//	@Summary		Add conversation
//	@Description	Add conversation
//	@Tags			conversation
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	string
//	@Failure		500	{object}	string
func AddConversation(c *gin.Context) {
	// bind json body
	var conv openai.Conversation
	if err := c.ShouldBindJSON(&conv); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// call backend
	err := Backend.AddConversation(&conv)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, nil)
}

// SystemRoles API
// ListSystemRoles doc
//
//	@Router			/system_roles [get]
//	@Summary		List system roles
//	@Description	List system roles
//	@Tags			system_role
//	@Accept			json
//	@Produce		json
//	@Success		200	{array}		openai.SystemRole
//	@Failure		500	{object}	string
func ListSystemRoles(c *gin.Context) {

}

// AddSystemRole doc
//
//	@Router			/system_roles [post]
//	@Summary		Add system role
//	@Description	Add system role
//	@Tags			system_role
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	openai.SystemRole
//	@Failure		500	{object}	string
func AddSystemRole(c *gin.Context) {
	// bind openai.SystemRole
	var role openai.SystemRole
	if err := c.ShouldBindJSON(&role); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// call backend
	err := Backend.AddSystemRole(&role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, nil)
}

// GetSystemRole doc
//
//	@Router			/system_roles/{id} [get]
//	@Summary		Get system role
//	@Description	Get system role
//	@Tags			system_role
//	@Accept			json
//	@Produce		json
//	@Param			id	path		int	true	"System Role ID"
//	@Success		200	{object}	openai.SystemRole
//	@Failure		500	{object}	string
func GetSystemRole(c *gin.Context) {
	// bind path param: id
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}
	// convert string to uint
	idUint, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is invalid"})
		return
	}
	// call backend
	role, err := Backend.GetSystemRole(uint(idUint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, role)
}

// Messages API
// AddMessages doc
//
//	@Router			/messages [post]
//	@Summary		Add messages
//	@Description	Add messages
//	@Tags			message
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	string
//	@Failure		500	{object}	string
func AddMessage(c *gin.Context) {
	// bind ChatCompletionMessage
	var msg openai.ChatCompletionMessage
	if err := c.ShouldBindJSON(&msg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// call backend
	err := Backend.AddMessages([]openai.ChatCompletionMessage{msg})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, nil)
}

// GetMessage doc
//
//	@Router			/messages/{id} [get]
//	@Summary		Get message
//	@Description	Get message
//	@Tags			message
//	@Accept			json
//	@Produce		json
//	@Param			id	path		int	true	"Message ID"
//	@Success		200	{object}	openai.ChatCompletionMessage
//	@Failure		500	{object}	string
func GetMessage(c *gin.Context) {
	// get id
	id := c.Param("id")
	//convert string to uint
	idUint, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is invalid"})
		return
	}
	// call backend
	msg, err := Backend.GetMessage(uint(idUint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, msg)
}

func initDB(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open("chat.db"), &gorm.Config{})
	if err != nil {
		log.Print(err)
		return nil, err
	}
	// Migrate the schema
	if err := db.AutoMigrate(&openai.Conversation{}, &openai.ChatCompletionMessage{}); err != nil {
		log.Print(err)
		return nil, err
	}
	return db, nil
}

func main() {
	// init backend
	dbPath := ""
	openaiToken := ""
	db, err := initDB(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	Backend = openai.NewGpt3p5(db, openaiToken)

	// create gin handler
	r := gin.Default()

	srv := &http.Server{
		Addr:    ":80",
		Handler: r,
	}

	// swagger API docs
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	r.GET("/api/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	go func() {
		// service connections
		log.Print("start server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	// graceful shutdown
	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal, 1)
	// kill (no param) default send syscanll.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall. SIGKILL but can"t be catch, so don't need add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE)
	sig := <-quit
	log.Print("Shutdown Server ...")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server Shutdown:", err)
	}

	<-ctx.Done()
	log.Print("Server exiting by signal " + sig.String())

}
