package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

type User struct {
	TelegramID int64
	UniqueID   string
}

type Message struct {
	ID         int
	FromAnonID int64
	ToOwnerID  int64
	Text       string
	Timestamp  string
	IsRead     bool
}

type Session struct {
	AnonID   int64
	UniqueID string
}

func main() {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Initialize database
	db, err := sql.Open("sqlite3", "./anonbot.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create tables
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			telegram_id INTEGER PRIMARY KEY,
			unique_id TEXT NOT NULL UNIQUE
		);
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			from_anon_id INTEGER,
			to_owner_id INTEGER,
			text TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_read BOOLEAN DEFAULT FALSE
		);
		CREATE TABLE IF NOT EXISTS sessions (
			anon_id INTEGER PRIMARY KEY,
			unique_id TEXT NOT NULL,
			FOREIGN KEY(unique_id) REFERENCES users(unique_id)
		);
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Initialize Telegram bot
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN not set in .env file")
	}
	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatal(err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	// Set up webhook or polling
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// Handle updates
	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Handle /start command
		if update.Message.IsCommand() && update.Message.Command() == "start" {
			uniqueID := update.Message.CommandArguments()
			if uniqueID == "" {
				// Generate new unique link for user
				newUUID := uuid.New().String()
				_, err := db.Exec("INSERT OR REPLACE INTO users (telegram_id, unique_id) VALUES (?, ?)",
					update.Message.From.ID, newUUID)
				if err != nil {
					log.Printf("Error saving user: %v", err)
					continue
				}

				link := fmt.Sprintf("t.me/%s?start=%s", bot.Self.UserName, newUUID)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Your unique link: "+link)
				bot.Send(msg)
			} else {
				// Check if uniqueID exists
				var ownerID int64
				err := db.QueryRow("SELECT telegram_id FROM users WHERE unique_id = ?", uniqueID).Scan(&ownerID)
				if err != nil {
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Invalid link.")
					bot.Send(msg)
					continue
				}

				// Save session for anonymous user
				_, err = db.Exec("INSERT OR REPLACE INTO sessions (anon_id, unique_id) VALUES (?, ?)",
					update.Message.From.ID, uniqueID)
				if err != nil {
					log.Printf("Error saving session: %v", err)
					continue
				}

				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Отправь анонимное сообщение:")
				bot.Send(msg)
			}
			continue
		}

		// Handle anonymous messages
		if update.Message.Text != "" {
			// Look up session to get unique_id
			var uniqueID string
			err := db.QueryRow("SELECT unique_id FROM sessions WHERE anon_id = ?", update.Message.From.ID).Scan(&uniqueID)
			if err != nil {
				log.Printf("Error finding session: %v", err)
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Пожалуйста начни с валидной ссылкой /start <unique_id>")
				bot.Send(msg)
				continue
			}

			// Find owner
			var ownerID int64
			err = db.QueryRow("SELECT telegram_id FROM users WHERE unique_id = ?", uniqueID).Scan(&ownerID)
			if err != nil {
				log.Printf("Error finding owner: %v", err)
				continue
			}

			// Save message
			_, err = db.Exec("INSERT INTO messages (from_anon_id, to_owner_id, text) VALUES (?, ?, ?)",
				update.Message.From.ID, ownerID, update.Message.Text)
			if err != nil {
				log.Printf("Error saving message: %v", err)
				continue
			}

			// Forward to owner
			msg := tgbotapi.NewMessage(ownerID, "Тебе пришло сообщение: "+update.Message.Text)
			bot.Send(msg)
		}
	}
}
