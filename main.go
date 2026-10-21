package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/medkvadrat/medkvadrat-max-bot/internal/config"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/gateway"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/handlers"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/linkallow"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/maxclient"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/notices"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/reminders"
	"github.com/medkvadrat/medkvadrat-max-bot/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Ошибка загрузки конфига: %v", err)
	}

	store, err := storage.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Ошибка инициализации БД: %v", err)
	}
	defer store.Close()
	log.Printf("SQLite инициализирована: %s", cfg.DBPath)

	linkAllow := linkallow.New(cfg.BotLinkAllowlistPatients)
	log.Printf("link allowlist: %d patients", len(linkAllow))
	if len(linkAllow) == 0 {
		log.Printf("WARNING: BOT_LINK_ALLOWLIST_PATIENTS is empty — linking disabled for everyone")
	}
	if n, err := store.PurgeLinksOutsideAllowlist(linkAllow); err != nil {
		log.Printf("purge user_links outside allowlist: %v", err)
	} else if n > 0 {
		log.Printf("purge user_links outside allowlist: removed %d", n)
	}

	mc := maxclient.New(cfg.MaxBotToken)
	gw := gateway.New(cfg.GatewayURL, cfg.GatewayToken)

	// Проверяем, что токен живой и API отвечает
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	me, err := mc.GetMe(ctx)
	if err != nil {
		log.Fatalf("MAX API не отвечает (проверь MAX_BOT_TOKEN): %v", err)
	}
	log.Printf("Бот авторизован: %s (@%s), user_id=%d",
		me.Name, me.Username, me.UserID)

	h := handlers.New(mc, gw, store)
	h.LinkAllow = linkAllow
	h.KeyboardDebug = cfg.KeyboardDebug
	if cfg.KeyboardDebug {
		log.Println("KEYBOARD_DEBUG=true: /start и «тест-кнопки» шлют demo-клавиатуру")
	}

	if cfg.ReminderEnabled {
		runner := &reminders.Runner{
			Gateway:   gw,
			Storage:   store,
			Messenger: reminders.NewMaxMessenger(mc),
			Allowlist:     cfg.ReminderAllowlistPatients,
			LinkAllowlist: cfg.BotLinkAllowlistPatients,
		}
		go reminders.Start(ctx, cfg.ReminderTick, runner)
		log.Printf("Планировщик напоминаний запущен (тик %s)", cfg.ReminderTick)
	} else {
		log.Println("Планировщик напоминаний отключён (REMINDER_ENABLED=false)")
	}

	if cfg.BookingNoticeEnabled {
		if len(cfg.ReminderAllowlistPatients) == 0 {
			log.Printf("WARNING: BOOKING_NOTICE_ENABLED but REMINDER_ALLOWLIST_PATIENTS is empty — notices disabled")
		}
		go notices.Start(ctx, &notices.Runner{
			Gateway:       gw,
			Storage:       store,
			Sender:        textSender{mc},
			LinkAllow:     cfg.BotLinkAllowlistPatients,
			ReminderAllow: cfg.ReminderAllowlistPatients,
		})
		log.Println("Уведомления о новых записях включены (тик 60s)")
	}

	log.Println("Long polling запущен. Ctrl+C для остановки.")
	runLongPolling(ctx, mc, h)
	log.Println("Остановка завершена")
}

type textSender struct{ c *maxclient.Client }

func (s textSender) SendText(ctx context.Context, userID int64, text string) error {
	return s.c.SendToUser(ctx, userID, text)
}

// runLongPolling — основной цикл.
// Держит соединение с MAX до 90 секунд, получает батч обновлений,
// отдаёт их хендлерам и переходит к следующей итерации.
func runLongPolling(ctx context.Context, mc *maxclient.Client, h *handlers.Handler) {
	var marker int64 = 0

	for {
		if ctx.Err() != nil {
			return
		}

		resp, err := mc.GetUpdates(ctx, marker, 90)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("GetUpdates: %v", err)
			// Бэкофф, чтобы не забить логи и не DoS-ить MAX при недоступности
			time.Sleep(3 * time.Second)
			continue
		}

		for i := range resp.Updates {
			handleOne(ctx, h, &resp.Updates[i])
		}
		marker = resp.Marker
	}
}

// handleOne — один апдейт в recover-обёртке, чтобы паника в хендлере
// не валила весь long-polling цикл.
func handleOne(ctx context.Context, h *handlers.Handler, u *maxclient.Update) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic в хендлере (update_type=%s): %v", u.UpdateType, r)
		}
	}()

	var err error
	switch u.UpdateType {
	case "bot_started":
		err = h.OnBotStarted(ctx, u)
	case "message_created":
		err = h.OnMessageCreated(ctx, u)
	case "message_callback":
		err = h.OnMessageCallback(ctx, u)
	default:
		log.Printf("пропускаем update_type=%s", u.UpdateType)
		return
	}
	if err != nil {
		log.Printf("ошибка обработки %s: %v", u.UpdateType, err)
	}
}
