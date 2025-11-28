package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/digkill/TGStickerBot/internal/service"
)

type Server struct {
	addr     string
	username string
	password string
	log      *slog.Logger
	users    *service.UserService
	plans    *service.PlanService
	promos   *service.PromoService
	payments *service.PaymentService
	bot      *tgbotapi.BotAPI
	router   *chi.Mux
}

func NewServer(addr, username, password string, log *slog.Logger, users *service.UserService, plans *service.PlanService, promos *service.PromoService, payments *service.PaymentService, bot *tgbotapi.BotAPI) *Server {
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
		plans:    plans,
		promos:   promos,
		payments: payments,
		bot:      bot,
		router:   r,
	}
	r.Post("/webhook/yookassa", s.handleYooKassaWebhook)
	r.Group(func(protected chi.Router) {
		protected.Use(s.basicAuthMiddleware())
		protected.Post("/broadcast", s.handleBroadcast)
		protected.Route("/plans", func(r chi.Router) {
			r.Get("/", s.handleListPlans)
			r.Post("/", s.handleCreatePlan)
			r.Put("/{id}", s.handleUpdatePlan)
			r.Delete("/{id}", s.handleDeletePlan)
		})
		protected.Route("/promo-codes", func(r chi.Router) {
			r.Get("/", s.handleListPromos)
			r.Post("/", s.handleCreatePromo)
			r.Put("/{id}", s.handleUpdatePromo)
			r.Delete("/{id}", s.handleDeletePromo)
		})
	})
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

func (s *Server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := s.plans.List(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, plans)
}

func (s *Server) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	var req planRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	input := service.CreatePlanInput{
		Title:           req.Title,
		Description:     req.Description,
		Currency:        req.Currency,
		PriceMinorUnits: req.PriceMinorUnits,
		Credits:         req.Credits,
		IsActive:        req.IsActive,
	}
	plan, err := s.plans.Create(r.Context(), input)
	if err != nil {
		s.badRequest(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, plan)
}

func (s *Server) handleUpdatePlan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req planUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	input := service.UpdatePlanInput{
		Title:           req.Title,
		Description:     req.Description,
		Currency:        req.Currency,
		PriceMinorUnits: req.PriceMinorUnits,
		Credits:         req.Credits,
		IsActive:        req.IsActive,
	}
	plan, err := s.plans.Update(r.Context(), id, input)
	if err != nil {
		s.badRequest(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleDeletePlan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.plans.Delete(r.Context(), id); err != nil {
		s.badRequest(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPromos(w http.ResponseWriter, r *http.Request) {
	promos, err := s.promos.List(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, promos)
}

func (s *Server) handleCreatePromo(w http.ResponseWriter, r *http.Request) {
	var req promoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Code == "" || req.MaxUses <= 0 {
		http.Error(w, "code and max_uses required", http.StatusBadRequest)
		return
	}
	promo, err := s.promos.Create(r.Context(), req.Code, req.MaxUses)
	if err != nil {
		s.badRequest(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, promo)
}

func (s *Server) handleUpdatePromo(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req promoUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	existing, err := s.promos.GetByID(r.Context(), id)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if existing == nil {
		http.Error(w, "promo not found", http.StatusNotFound)
		return
	}
	code := existing.Code
	if req.Code != nil && *req.Code != "" {
		code = *req.Code
	}
	maxUses := existing.MaxUses
	if req.MaxUses != nil && *req.MaxUses > 0 {
		maxUses = *req.MaxUses
	}
	uses := existing.Uses
	if req.Uses != nil && *req.Uses >= 0 {
		uses = *req.Uses
	}
	if uses > maxUses {
		http.Error(w, "uses cannot exceed max_uses", http.StatusBadRequest)
		return
	}
	promo, err := s.promos.Update(r.Context(), id, code, maxUses, uses)
	if err != nil {
		s.badRequest(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, promo)
}

func (s *Server) handleDeletePromo(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := s.promos.Delete(r.Context(), id); err != nil {
		s.badRequest(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleYooKassaWebhook is public endpoint for YooKassa payment status updates.
// Expects JSON payload from YooKassa; on success credits the user and updates payment status.
func (s *Server) handleYooKassaWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body error", http.StatusBadRequest)
		return
	}
	if err := s.payments.HandleYooKassaWebhook(r.Context(), body); err != nil {
		s.log.Error("yookassa webhook", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) basicAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != s.username || pass != s.password {
				w.Header().Set("WWW-Authenticate", `Basic realm="stickerbot"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) badRequest(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.log.Error("admin handler error", "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func parseID(value string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}

type planRequest struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	Currency        string `json:"currency"`
	PriceMinorUnits int    `json:"price_minor_units"`
	Credits         int    `json:"credits"`
	IsActive        *bool  `json:"is_active"`
}

type planUpdateRequest struct {
	Title           *string `json:"title"`
	Description     *string `json:"description"`
	Currency        *string `json:"currency"`
	PriceMinorUnits *int    `json:"price_minor_units"`
	Credits         *int    `json:"credits"`
	IsActive        *bool   `json:"is_active"`
}

type promoRequest struct {
	Code    string `json:"code"`
	MaxUses int    `json:"max_uses"`
}

type promoUpdateRequest struct {
	Code    *string `json:"code"`
	MaxUses *int    `json:"max_uses"`
	Uses    *int    `json:"uses"`
}
