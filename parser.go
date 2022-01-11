package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gocolly/colly"
	"github.com/jackc/pgx/v4/pgxpool"
)

const DOMAIN string = "dictionary.cambridge.org"
const DOMAIN_URL string = "https://dictionary.cambridge.org"
const START_URL string = "https://dictionary.cambridge.org/ru/%D1%81%D0%BB%D0%BE%D0%B2%D0%B0%D1%80%D1%8C/%D0%B0%D0%BD%D0%B3%D0%BB%D0%B8%D0%B9%D1%81%D0%BA%D0%B8%D0%B9/"
const DB_USER string = "postgres"
const DB_PASSWORD string = "password"
const DB_HOST string = "localhost"
const DB_PORT int = 5432
const DB_NAME string = "testdb"

// 	Значение LOGS_MODE позволяет управлять выводом логов.
//	0 - на экран выводится количество собранных фраз
// 	1 - на экран выводится сообщение с последней собранной фразой
//  2 - на экран выводится вся собранная информация

const LOGS_MODE = 1

var count = 0

func startParsing(conn *pgxpool.Pool, url string) {
	collectors := createCollectors()
	setupCollectors(collectors, conn)
	collectors[1].Visit(url)
}

func createCollectors() []colly.Collector {
	collectors := make([]colly.Collector, 4)
	for i := range collectors {
		collectors[i] = *colly.NewCollector(colly.AllowedDomains(DOMAIN))
	}
	return collectors
}

func setupCollectors(collectors []colly.Collector, conn *pgxpool.Pool) {
	//	В англоязычном словаре dictionary.cambridge.org все слова разделены по первым буквам в алфавитном порядке. Для каждой буквы - отдельная страница.
	//	На странице букв слова также разделены на группы. Для каждой группы также предусмотрена отдельная страница, на которой уже и расположены необходимые нам слова/фразы.
	//	Коллекторы [1-3] предназначены для сбора ссылок на эти промежуточные страницы и перехода непосредственно на страницу конкретного слова/фразы.
	//	Коллектор [0] предназначен для сбора необходимой нам информации со страницы слова/фразы.
	//	Поиск осуществляется по селекторам.

	collectors[1].OnHTML(".hbr", func(element *colly.HTMLElement) {
		url := DOMAIN_URL + element.Attr("href")
		collectors[2].Visit(url)
	})

	collectors[2].OnHTML(".dil", func(element *colly.HTMLElement) {
		url := element.Attr("href")
		collectors[3].Visit(url)
	})

	collectors[3].OnHTML(".tc-bd", func(element *colly.HTMLElement) {
		if element.Attr("href") != "" && !strings.Contains(element.Attr("href"), "https://") {
			url := DOMAIN_URL + element.Attr("href")
			collectors[0].Visit(url)
		}
	})

	collectors[0].OnHTML(".entry-body__el", func(e *colly.HTMLElement) {
		parseEntry(conn, e)
		printSeparator()
	})
}

func parseEntry(conn *pgxpool.Pool, e *colly.HTMLElement) {
	title, pos, pron_uk, pron_us := collectHeaderData(e)
	count++
	printHeaderData(title, pos, pron_uk, pron_us)
	word_id := insertHeaderData(conn, title, pos, pron_uk, pron_us)
	parseDefinitionBody(conn, e, word_id)
}

func collectHeaderData(e *colly.HTMLElement) (string, string, string, string) {
	title := e.DOM.Find("div.entry-body__el > div.pos-header > div.di-title").Text()
	pos := e.DOM.Find("div.entry-body__el > div.pos-header > div.dpos-g > span.dpos").Text()
	pron_uk := e.DOM.Find("div.entry-body__el > div.pos-header > span.uk > span.dpron > span.dipa").Text()
	pron_us := e.DOM.Find("div.entry-body__el > div.pos-header > span.us > span.dpron > span.dipa").Text()

	return title, pos, pron_uk, pron_us
}

func insertHeaderData(conn *pgxpool.Pool, title string, pos string, pron_uk string, pron_us string) uint64 {
	var id uint64
	err := conn.QueryRow(context.Background(), "INSERT INTO word (word, part_of_speech, transcription_uk, transcription_us) VALUES ($1, $2, $3, $4) RETURNING id", title, pos, pron_uk, pron_us).Scan(&id)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return id
}

func parseDefinitionBody(conn *pgxpool.Pool, e *colly.HTMLElement, word_id uint64) {
	//	Данная функция выводит на экран собранные определения и примеры, а также загружает их в БД.
	//	Можно было разделить эти задачи на две функции, но в таком случае пришлось бы сохранять определения и примеры в отдельные слайсы,
	// 	после чего пробегаться по каждому элементу срезов и загружать всё в БД.
	// 	Это было бы удобнее с точки зрения модицикации кода и его дальнейшего использования.
	// 	Однако это бы замедлило скорость сбора информации, поэтому было принято решение сразу загружать всё в БД, без добавление в слайс или структуру.

	e.ForEach("div.sense-body > div.ddef_block", func(_ int, s *colly.HTMLElement) {
		def := collectDefinition(s)
		def_id := insertDefinition(conn, word_id, def)
		printDefinition(def)

		s.ForEach("div.dexamp", func(_ int, ds *colly.HTMLElement) {
			examp := collectExample(ds)
			insertExample(conn, def_id, examp)
			printExample(examp)
		})
	})
}

func collectDefinition(e *colly.HTMLElement) string {
	def := e.DOM.Find("div.def").Text()
	def = formatSentence(def)

	return def
}

func insertDefinition(conn *pgxpool.Pool, word_id uint64, def string) uint64 {
	var def_id uint64
	err := conn.QueryRow(context.Background(), "INSERT INTO definition (word_id, definition) VALUES ($1, $2) RETURNING id", word_id, def).Scan(&def_id)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return def_id
}

func collectExample(e *colly.HTMLElement) string {
	examp := e.Text
	examp = formatSentence(examp)

	return examp
}

func insertExample(conn *pgxpool.Pool, def_id uint64, example string) {
	conn.QueryRow(context.Background(), "INSERT INTO example (definition_id, example) VALUES ($1, $2)", def_id, example).Scan()
}

func printHeaderData(title string, pos string, pron_uk string, pron_us string) {
	switch LOGS_MODE {
	case 0:
		fmt.Fprintf(os.Stdout, "Count of collected words: %d\n", count)
	case 1:
		fmt.Fprintf(os.Stdout, "Count of collected words: %d		# ", count)
		fmt.Fprintf(os.Stdout, "Collected: %s\n", title)
	case 2:
		fmt.Fprintf(os.Stdout, "Count of collected words: %d\n", count)
		fmt.Fprintf(os.Stdout, "Title: %s\n", title)
		fmt.Fprintf(os.Stdout, "Part of speech: %s\n", pos)
		fmt.Fprintf(os.Stdout, "Pronunciation UK: %s\n", pron_uk)
		fmt.Fprintf(os.Stdout, "Pronunciation US: %s\n", pron_us)
	}
}

func printDefinition(def string) {
	if LOGS_MODE == 2 {
		fmt.Fprintf(os.Stdout, "Definition: %s\n", def)
	}
}

func printExample(examp string) {
	if LOGS_MODE == 2 {
		fmt.Fprintf(os.Stdout, "			 • %s\n", examp)
	}
}

func printSeparator() {
	if LOGS_MODE == 2 {
		fmt.Fprintf(os.Stdout, "----------------------------------------\n")
	}
}

func printFinalMessage() {
	fmt.Fprintf(os.Stdout, "All words are collected!\n")
	fmt.Fprintf(os.Stdout, "Count of collected words: %d\n", count)
}

func formatSentence(s string) string {
	s = strings.Replace(s, "→", "", 1)
	s = strings.TrimSpace(s)

	if s[len(s)-1] == ':' {
		s = strings.Replace(s, ":", "", -1)
	} else if s[len(s)-1] == '.' {
		s = strings.Replace(s, ".", "", -1)
	}

	return s
}

func main() {
	DB_URL := fmt.Sprintf("postgres://%s:%s@%s:%d/%s", DB_USER, DB_PASSWORD, DB_HOST, DB_PORT, DB_NAME)

	conn, err := pgxpool.Connect(context.Background(), DB_URL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}

	startParsing(conn, START_URL)
	printFinalMessage()
}
