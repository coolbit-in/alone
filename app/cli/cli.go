package main

import (
	"alone/openai"
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	l "log"

	"github.com/gin-gonic/gin"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var (
	log *l.Logger
)

func init() {
	log = l.New(os.Stderr, "", l.LstdFlags|l.Lshortfile)
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

type Session struct {
	UserID        uint `json:"user_id"`
	ConvID        uint `json:"conv_id"`
	EnableContext bool `json:"enable_context"`
}
type SynologyChatBot struct {
	backend  openai.GptBackend
	router   *gin.Engine
	botToken string
	//CurrentCoversationID uint
	nasDomain string
	// enableContext bool
	sessions sync.Map
}

func (bot *SynologyChatBot) GetSession(userID uint) (Session, bool) {
	var session Session
	v, ok := bot.sessions.Load(userID)
	if !ok {
		return session, false
	}
	session, ok = v.(Session)
	if !ok {
		return session, false
	}
	return session, true
}

func (bot *SynologyChatBot) SetSession(userID uint, session Session) {
	bot.sessions.Store(userID, session)
}

func (bot *SynologyChatBot) DumpSessions(filePath string) error {
	// Dump sessionMap to a byte buffer
	var sessionBuf bytes.Buffer
	enc := gob.NewEncoder(&sessionBuf)
	bot.sessions.Range(func(k, v interface{}) bool {
		session := v.(Session)
		if err := enc.Encode(session); err != nil {
			return false
		}
		return true
	})

	// Write byte buffer to file
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(sessionBuf.Bytes()); err != nil {
		return err
	}
	return nil
}

func (bot *SynologyChatBot) LoadSessions(filePath string) error {
	// Read session data from file into a byte buffer
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()
	sessionBytes, err := ioutil.ReadAll(file)
	if err != nil {
		return err
	}
	// Decode the byte buffer and load sessions into sessionMap
	var decodedSession Session
	dec := gob.NewDecoder(bytes.NewReader(sessionBytes))
	for dec.Decode(&decodedSession) == nil {
		log.Print(decodedSession.UserID, decodedSession.ConvID, decodedSession.EnableContext)
		bot.sessions.Store(decodedSession.UserID, decodedSession)
	}
	return nil
}

func NewSynologyChatBot(backend openai.GptBackend, token string, nasDomain string) *SynologyChatBot {
	bot := &SynologyChatBot{
		backend:   backend,
		router:    gin.Default(),
		botToken:  token,
		nasDomain: nasDomain,
		sessions:  sync.Map{},
	}
	bot.LoadSessions("sessions.gob")
	return bot
}

// EnableContext enable converstion context for gpt
func (bot *SynologyChatBot) EnableContext(userID uint) {
	session, bool := bot.GetSession(userID)
	if !bool {
		bot.CreateSession(userID)
	}
	session.EnableContext = true
	bot.SetSession(userID, session)
}

// DisableContext disable converstion context for gpt
func (bot *SynologyChatBot) DisableContext(userID uint) {
	session, bool := bot.GetSession(userID)
	if !bool {
		bot.CreateSession(userID)
	}
	session.ConvID = 0
	session.EnableContext = false
	bot.SetSession(userID, session)
}

// ResetConversationr reset current conversation,
// and will generate a new conversation id in next request(only in EnableContext mode)
func (bot *SynologyChatBot) ResetConversation(userID uint) {
	session, bool := bot.GetSession(userID)
	if !bool {
		bot.CreateSession(userID)
	}
	session.ConvID = 0
	bot.SetSession(userID, session)
}

// CreateSession creates a new session for a user and adds it to the sessions map. It is called when a user starts a new conversation with the bot.
func (bot *SynologyChatBot) CreateSession(userID uint) {
	session := Session{
		UserID:        userID,
		ConvID:        0,
		EnableContext: true,
	}
	bot.SetSession(userID, session)
}

func (bot *SynologyChatBot) payloadEncode(input string) []string {
	r := []rune(input)
	c := len(r)/2000 + 1
	res := make([]string, c)
	// split the input into multiple messages
	for i, char := range r {
		pos := i / 2000
		if char == '"' {
			res[pos] += string(char)
		} else if strings.ContainsRune("!#$%&'()*+,/:;=?@[]", char) {
			res[pos] += url.QueryEscape(string(char))
		} else {
			res[pos] += string(char)
		}
	}
	return res
}

// SimpleAnswer replay text to user
func (bot *SynologyChatBot) SimpleAnswer(userIds []uint, text string) error {
	baseURL := bot.nasDomain + "/webapi/entry.cgi"
	queryParams := url.Values{}
	queryParams.Set("api", "SYNO.Chat.External")
	queryParams.Set("method", "chatbot")
	queryParams.Set("version", "2")
	queryParams.Set("token", bot.botToken)
	uri := baseURL + "?" + queryParams.Encode()

	type AnswerRequest struct {
		Text    string `json:"text,omitempty"`
		UserIds []uint `json:"user_ids,omitempty"`
	}

	type SynChatResponse struct {
		Data struct {
			Fail interface{} `json:"fail"`
			Succ struct {
				UserIDPostMap map[string]int64 `json:"user_id_post_map"`
			} `json:"succ"`
			Error struct {
				Code   int    `json:"code"`
				Errors string `json:"errors"`
			} `json:"error"`
		} `json:"data"`
		Success bool `json:"success"`
	}

	for _, encoded := range bot.payloadEncode(text) {
		if len(encoded) == 0 {
			continue
		}
		payload := AnswerRequest{
			Text:    encoded,
			UserIds: userIds,
		}
		body, _ := json.Marshal(payload)
		body = append([]byte("payload="), body...)
		log.Print(string(body))

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "POST", uri, bytes.NewReader(body))
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Print(err)
			return err
		}
		defer resp.Body.Close()
		log.Print(resp.Status)
		synChatResponse := SynChatResponse{}
		body, _ = ioutil.ReadAll(resp.Body)
		fmt.Println(string(body))
		if err := json.Unmarshal(body, &synChatResponse); err != nil {
			log.Print(err)
			return err
		}
		fmt.Println(synChatResponse)
		if !synChatResponse.Success {
			err := fmt.Errorf("error: %v", synChatResponse)
			log.Print(err)
			return err
		}
	}
	return nil
}

func (bot *SynologyChatBot) price(tokenNum int) float64 {
	price := 0.002
	return float64(tokenNum) * price / 1000
}

func (bot *SynologyChatBot) prefix(userID uint, answer openai.ChatCompletionMessage) string {
	session, bool := bot.GetSession(userID)
	if !bool {
		return ""
	}
	contextFlag := "disable"
	if session.EnableContext {
		contextFlag = "enable"
	}
	total := answer.PromptTokens + answer.CompletionTokens
	prefix := fmt.Sprintf("[conv_id: %d, token: %d, cost: $%f, context: %s]\n",
		answer.ConversationID, total, bot.price(total), contextFlag)
	return prefix
}

// Answer make a http request to bot's ingoing url, payload is ChatComplationMessage
func (bot *SynologyChatBot) Answer(userIds []uint, answer openai.ChatCompletionMessage) error {
	prefix := bot.prefix(userIds[0], answer)
	answer.Content = prefix + answer.Content
	return bot.SimpleAnswer(userIds, answer.Content)
}

func (bot *SynologyChatBot) Run(address, port string) {
	type SynChatRequest struct {
		Token     string `form:"token"`
		UserID    uint   `form:"user_id"`
		Username  string `form:"username"`
		PostID    string `form:"post_id"`
		ThreadID  string `form:"thread_id"`
		Timestamp string `form:"timestamp"`
		Text      string `form:"text"`
	}
	bot.router.POST("/", func(c *gin.Context) {
		var requestBody SynChatRequest
		err := c.Bind(&requestBody)
		if err != nil {
			fmt.Println(err)
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusOK)
		go func() {
			session, ok := bot.GetSession(requestBody.UserID)
			if !ok {
				bot.CreateSession(requestBody.UserID)
				session, _ = bot.GetSession(requestBody.UserID)
			}
			answer, err := bot.backend.Send(session.ConvID, requestBody.Text)
			if err != nil {
				log.Print(err)
				return
			}
			if session.ConvID == 0 && session.EnableContext {
				session.ConvID = answer.ConversationID
				bot.SetSession(requestBody.UserID, session)
			}
			err = bot.Answer([]uint{requestBody.UserID}, answer)
			if err != nil {
				log.Print(err)
				return
			}
		}()
	})

	bot.router.POST("/botconf", func(c *gin.Context) {
		type RequestBody struct {
			Token    string `form:"token"`
			UserID   uint   `form:"user_id"`
			Username string `form:"username"`
			Text     string `form:"text"`
		}
		var requestBody RequestBody
		err := c.Bind(&requestBody)
		if err != nil {
			fmt.Println(err)
			c.Status(http.StatusBadRequest)
			return
		}
		command := strings.TrimPrefix(requestBody.Text, "/botconf ")
		log.Printf("command: %s", command)
		switch command {
		case "disable_context":
			bot.DisableContext(requestBody.UserID)
			bot.SimpleAnswer([]uint{requestBody.UserID}, "Context disabled")
		case "enable_context":
			bot.EnableContext(requestBody.UserID)
			bot.SimpleAnswer([]uint{requestBody.UserID}, "Context enabled")
		case "reset_conversation":
			bot.ResetConversation(requestBody.UserID)
			bot.SimpleAnswer([]uint{requestBody.UserID}, "Conversation Reseted")
		default:
			// do nothing
		}
		c.Status(http.StatusOK)
	})

	err := bot.router.Run(fmt.Sprintf("%s:%s", address, port))
	if err != nil {
		log.Fatal(err)
	}
}

type Config struct {
	SqlitePath  string `mapstructure:"sqlite_path,omitempty"`
	OpenaiToken string `mapstructure:"openai_token"`
	BotToken    string `mapstructure:"bot_token"`
	NasDomain   string `mapstructure:"nas_domain"`
	Address     string `mapstructure:"service_address,omitempty"`
	Port        string `mapstructure:"service_port,omitempty"`
	//DatabaseUrl string `mapstructure:"database_url"`
}

func initConfig(confPath string) Config {
	// Set the Viper package to read config.yaml.
	viper.SetConfigFile(confPath)
	// Read in the config file.
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatal(fmt.Errorf("fatal error config file: %s", err))
	}
	viper.BindPFlag("sqlite_path", pflag.Lookup("sqlite_path"))
	viper.BindPFlag("openai_token", pflag.Lookup("openai_token"))
	viper.BindPFlag("bot_token", pflag.Lookup("bot_token"))
	// Initialize the Config struct.
	var config Config
	// Unmarshal the config file into the Config struct.
	err = viper.Unmarshal(&config)
	if err != nil {
		log.Fatal(fmt.Errorf("error unmarshalling config: %s", err))
	}
	return config
}

func main() {
	var config Config
	confPath := *pflag.StringP("conf", "c", "config.yaml", "configure file path")
	//confPath := *flag.String("conf", "config.yaml", "config file path")
	pflag.String("sqlite_path", "", "sqlite file path")
	pflag.String("openai_token", "", "openai token")
	pflag.String("bot_token", "", "synology chat bot token")
	pflag.Parse()
	config = initConfig(confPath)
	fmt.Println(config)
	db, err := initDB(config.SqlitePath)
	if err != nil {
		log.Fatal(err)
	}
	backend := openai.NewGpt3p5(db, config.OpenaiToken)
	app := NewSynologyChatBot(backend, config.BotToken, config.NasDomain)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		app.DumpSessions("sessions.gob")
		os.Exit(0)
	}()
	app.Run(config.Address, config.Port)
}
