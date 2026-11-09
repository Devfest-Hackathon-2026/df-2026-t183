package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/gemini"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/httpapi"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/learning"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/speech"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/store"
	"github.com/yasusudas/ai-ni-oshieru/backend/internal/synthesis"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	project, location, model := os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GOOGLE_CLOUD_LOCATION"), os.Getenv("GEMINI_MODEL")
	originValue := os.Getenv("ALLOWED_ORIGINS")
	maxTokens, err := strconv.ParseInt(os.Getenv("GEMINI_MAX_OUTPUT_TOKENS"), 10, 32)
	if project == "" || location == "" || model == "" || originValue == "" || err != nil || maxTokens <= 0 {
		return fmt.Errorf("GOOGLE_CLOUD_PROJECT、GOOGLE_CLOUD_LOCATION、GEMINI_MODEL、ALLOWED_ORIGINS、正の GEMINI_MAX_OUTPUT_TOKENS を設定してください")
	}
	origins := strings.Split(originValue, ",")
	for i, origin := range origins {
		origin = strings.TrimSpace(origin)
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("ALLOWED_ORIGINS に無効なオリジンがあります")
		}
		origins[i] = origin
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("PORT は 1〜65535 の数値にしてください")
	}
	security, ttl, err := persistenceConfig()
	if err != nil {
		return err
	}
	var repository learning.Repository
	if security != nil {
		databaseID := os.Getenv("FIRESTORE_DATABASE_ID")
		if databaseID == "" {
			databaseID = "(default)"
		}
		startup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		db, err := firestore.NewClientWithDatabase(startup, project, databaseID)
		cancel()
		if err != nil {
			return fmt.Errorf("Firestore クライアントを初期化できません")
		}
		defer db.Close()
		repository = store.New(db, security, ttl)
	}
	client, err := gemini.New(context.Background(), project, location, model, int32(maxTokens))
	if err != nil {
		return fmt.Errorf("Gemini クライアントを初期化できません")
	}
	var validator speech.Validator
	var transcriber speech.Transcriber
	if speechModel := strings.TrimSpace(os.Getenv("SPEECH_MODEL")); speechModel != "" {
		speechLocation := strings.TrimSpace(os.Getenv("SPEECH_LOCATION"))
		if speechLocation == "" {
			speechLocation = "asia-southeast1"
		}
		transcriber, err = speech.NewClient(project, speechLocation, speechModel)
		if err != nil {
			return fmt.Errorf("Speech-to-Text クライアントを初期化できません")
		}
		validator, err = speech.NewValidator()
		if err != nil {
			log.Print("ffmpeg / ffprobe がないため音声文字起こしを無効にします")
			validator = nil
			transcriber = nil
		}
	}
	synthesizer, err := synthesis.NewClient()
	if err != nil {
		log.Print("Cloud Text-to-Speech を初期化できないため読み上げを無効にします")
	}
	server := &http.Server{Addr: net.JoinHostPort("0.0.0.0", port), Handler: httpapi.NewPersistentWithAudio(client, origins, security, repository, validator, transcriber, synthesizer), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("API サーバーをポート %s で起動します", port)
	return server.ListenAndServe()
}
