package main

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/color"
	"image/jpeg"
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/gorilla/websocket"
	"github.com/kbinani/screenshot"
)

type Message struct {
	RoomID   string `json:"room_id"`
	SenderID string `json:"sender_id"`
	Type     string `json:"type"`
	Payload  string `json:"payload"`
	IsFinal  bool   `json:"is_final"`
}

func main() {
	myApp := app.New()
	window := myApp.NewWindow("Screen Sharer (Reliable)")
	window.Resize(fyne.NewSize(450, 480))

	input := widget.NewEntry()
	input.SetText("ws://localhost:8080/ws")

	statusLabel := widget.NewLabel("Отключено")

	// Визуальный индикатор (сигнал) в виде цветного круга
	statusDot := canvas.NewCircle(color.NRGBA{R: 128, G: 128, B: 128, A: 255}) // Серый по дефолту
	statusDot.Resize(fyne.NewSize(12, 12))

	btn := widget.NewButton("Запустить трансляцию", nil)

	previewImage := canvas.NewImageFromResource(nil)
	previewImage.FillMode = canvas.ImageFillContain
	previewImage.SetMinSize(fyne.NewSize(400, 220))

	isLooping := false

	btn.OnTapped = func() {
		if isLooping {
			return
		}
		url := input.Text
		if url == "" {
			statusLabel.SetText("Введите валидный URL")
			statusDot.FillColor = color.NRGBA{R: 200, G: 0, B: 0, A: 255} // Красный
			statusDot.Refresh()
			return
		}

		// Запускаем бесконечный цикл стриминга с автореконнектом
		go startStreamingWithReconnect(url, statusLabel, statusDot, previewImage)
		isLooping = true
		btn.Disable()
		input.Disable() // Блокируем ввод во время работы
	}

	// Строка статуса: индикатор-круг и текст рядом
	statusContainer := container.NewHBox(
		widget.NewLabel("Статус:"),
		statusDot,
		statusLabel,
	)

	window.SetContent(container.NewVBox(
		widget.NewLabel("Введите WebSocket URL:"),
		input,
		btn,
		statusContainer,
		previewImage,
	))

	window.ShowAndRun()
}

func startStreamingWithReconnect(url string, label *widget.Label, dot *canvas.Circle, preview *canvas.Image) {
	var lastHash [16]byte

	for {
		// Обновляем статус: идет подключение
		label.SetText("Подключение...")
		dot.FillColor = color.NRGBA{R: 200, G: 200, B: 0, A: 255} // Желтый
		dot.Refresh()

		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			log.Printf("Ошибка подключения: %v. Повтор через 3 сек...", err)
			label.SetText("Ошибка связи. Повторный запуск...")
			dot.FillColor = color.NRGBA{R: 200, G: 0, B: 0, A: 255} // Красный
			dot.Refresh()

			time.Sleep(3 * time.Second) // Ждем перед следующей попыткой
			continue
		}

		// Успешно подключились
		label.SetText("На связи (Трансляция)")
		dot.FillColor = color.NRGBA{R: 0, G: 180, B: 0, A: 255} // Зеленый
		dot.Refresh()

		// Внутренний цикл отправки скриншотов
		for {
			time.Sleep(150 * time.Millisecond)

			img, err := screenshot.CaptureDisplay(0)
			if err != nil {
				log.Println("Ошибка скриншота:", err)
				continue
			}

			var buf bytes.Buffer
			err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 65})
			if err != nil {
				log.Println("Ошибка кодирования:", err)
				continue
			}
			imgBytes := buf.Bytes()

			currentHash := md5.Sum(imgBytes)
			if currentHash == lastHash {
				continue
			}

			base64Str := base64.StdEncoding.EncodeToString(imgBytes)

			msg := Message{
				RoomID:   "ignored",
				SenderID: "ignored",
				Type:     "image",
				Payload:  base64Str,
				IsFinal:  true,
			}

			jsonBytes, err := json.Marshal(msg)
			if err != nil {
				log.Println("Ошибка JSON:", err)
				continue
			}

			// Если отправка упала (сокет закрылся, пропал интернет), выходим из внутреннего цикла
			err = conn.WriteMessage(websocket.TextMessage, jsonBytes)
			if err != nil {
				log.Println("Соединение потеряно, инициируем переподключение...")
				conn.Close()
				break // Выход во внешний цикл для реконнекта
			}

			lastHash = currentHash

			preview.Resource = fyne.NewStaticResource("preview.jpg", imgBytes)
			preview.Refresh()
		}
	}
}

func startStreaming(url string, label *widget.Label, preview *canvas.Image) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		label.SetText(fmt.Sprintf("Ошибка: %v", err))
		return
	}
	defer conn.Close()
	label.SetText("Статус: Трансляция экрана...")

	var lastHash [16]byte

	for {
		time.Sleep(1000 * time.Millisecond)

		img, err := screenshot.CaptureDisplay(0)
		if err != nil {
			log.Println("Ошибка скриншота:", err)
			continue
		}

		var buf bytes.Buffer
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 65})
		if err != nil {
			log.Println("Ошибка кодирования:", err)
			continue
		}
		imgBytes := buf.Bytes()

		currentHash := md5.Sum(imgBytes)
		if currentHash == lastHash {
			continue
		}

		// 1. Кодируем бинарный JPEG в строку Base64
		base64Str := base64.StdEncoding.EncodeToString(imgBytes)

		// 2. Формируем структуру JSON для сервера
		msg := Message{
			RoomID:   "ignored", // Сервер сам перезапишет
			SenderID: "ignored", // Сервер сам перезапишет
			Type:     "image",
			Payload:  base64Str,
			IsFinal:  true,
		}

		// 3. Сериализуем структуру в массив байт JSON
		jsonBytes, err := json.Marshal(msg)
		if err != nil {
			log.Println("Ошибка создания JSON:", err)
			continue
		}

		// 4. Отправляем как ТЕКСТОВОЕ сообщение (TextMessage)
		err = conn.WriteMessage(websocket.TextMessage, jsonBytes)
		if err != nil {
			label.SetText("Статус: Соединение разорвано")
			break
		}

		lastHash = currentHash

		// Локальное превью обновляем из исходных байт (так быстрее)
		preview.Resource = fyne.NewStaticResource("preview.jpg", imgBytes)
		preview.Refresh()
	}
}
