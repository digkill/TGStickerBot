package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/example/stickerbot/internal/service"
)

type Server struct {
	addr     string
	username string
	password string
	log      *slog.Logger
	users    *service.UserService
	bot      *tgbotapi.BotAPI
	router   *chi.Mux
}

func NewServer(addr, username, password string, log *slog.Logger, users *service.UserService, bot *tgbotapi.BotAPI) *Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	s := &Server{
		addr:     addr,
		username: username,
		password: password,
		log:      log,
		users:    users,
		bot:      bot,
		router:   r,
	}
	r.Post("/broadcast", s.basicAuth(s.handleBroadcast))
	return s
}

func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:         s.addr,
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			s.log.Error("admin shutdown error", "err", err)
		}
	}()

	s.log.Info("admin panel listening", "addr", s.addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("admin listen: %w", err)
	}
	return nil
}

type broadcastRequest struct {
	Message string `json:"message"`
}

func (s *Server) handleBroadcast(w http.ResponseWriter, r *http.Request) {
	var req broadcastRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	ids, err := s.users.ListTelegramIDs(ctx)
	if err != nil {
		s.log.Error("list telegram ids", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	count := 0
	for _, id := range ids {
		msg := tgbotapi.NewMessage(id, req.Message)
		if _, err := s.bot.Send(msg); err != nil {
			s.log.Error("send broadcast", "user", id, "err", err)
			continue
		}
		count++
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sent":  count,
		"total": len(ids),
	})
}

func (s *Server) basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.username || pass != s.password {
			w.Header().Set("WWW-Authenticate", `Basic realm="stickerbot"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}
}
