package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"time"

	"github.com/go-redis/redis"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	"github.com/julienschmidt/httprouter"

	"github.com/mmcdole/gofeed"
)

type Article struct {
	gorm.Model
	PubDate  time.Time `gorm:"not null" json:"pub_date"`
	Title    string    `gorm:"not null;unique" json:"title"`
	Url      string    `gorm:"not null;unique" json:"url"`
	Source   Source    `gorm:"ForeignKey:SourceID" json:"source"`
	SourceID uint      `sql:"index"` // speed up
}

type Source struct {
	gorm.Model
	Country  string `gorm:"not null" json:"country"`
	Language string `gorm:"not null" json:"language"`
	Category string `gorm:"not null" json:"category"`
	Url      string `gorm:"not null;unique" json:"url"`
	Editor   string `gorm:"not null" json:"editor"`
	Logo     string `gorm:"not null" json:"logo"`
}

const cacheTime = 11

func init() {
	db, _ := gorm.Open("sqlite3", "feedme.db") // FIXME: upgrade to pg
	db.LogMode(false)
	db.CreateTable(&Source{}, &Article{})
	defer db.Close()
}

func redisClient() redis.Client {
	redis := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	return *redis
}

func updateSources() {
	db, _ := gorm.Open("sqlite3", "feedme.db")
	db.LogMode(false)
	defer db.Close()

	file, _ := os.Open("source.csv")
	r := csv.NewReader(file)
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		db.Create(&Source{Country: record[0], Language: record[1], Editor: record[2], Category: record[3], Url: record[4], Logo: record[5]})
	}
}

func crawlAndParseArticles() {
	db, _ := gorm.Open("sqlite3", "feedme.db")
	db.LogMode(false)
	defer db.Close()
	sources := []Source{}
	db.Find(&sources)
	for _, source := range sources {
		fp := gofeed.NewParser()
		feed, err := fp.ParseURL(source.Url)
		if err == nil {
			log.Printf("Parsing source: %v", source.Url)
			for _, item := range feed.Items {
				db.Save(&Article{Title: item.Title, Url: item.Link, Source: source, PubDate: *item.PublishedParsed})
			}
		}
	}

}

func loggerHttpRequests(w http.ResponseWriter, r *http.Request) {
	dump, err := httputil.DumpRequest(r, true)
	if err != nil {
		http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
		return
	}
	log.Printf("%q", dump)
}

func apiArticlesAll(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	redis := redisClient()
	articles, _ := redis.Get("articles").Result()
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(articles))
	loggerHttpRequests(w, r)
}

func apiArticlesPeriodValueRequest(w http.ResponseWriter, periodValue string, redis redis.Client) {
	articles, _ := redis.Get(periodValue).Result()
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(articles))
}

func apiArticlesPeriod(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	period := ps.ByName("period")
	redis := redisClient()
	switch period {
	case "today":
		apiArticlesPeriodValueRequest(w, "today", redis)
		loggerHttpRequests(w, r)
	case "month":
		apiArticlesPeriodValueRequest(w, "month", redis)
		loggerHttpRequests(w, r)
	}
}

func index(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	http.ServeFile(w, r, "views/index.html")
}

func feedsFirstImport(db gorm.DB, articles []Article, redis redis.Client) {
	log.Printf("Zero articles in DB, initiating first import")
	updateSources()
	crawlAndParseArticles()
	log.Printf("Import done!")
	r := feedsDefaultIndexQuery(db, articles)
	j, _ := json.Marshal(&r.Value)
	redis.Set("articles", j, cacheTime*time.Minute)
}

func feedsEmptyCache(redis redis.Client) {
	redis.Del("articles")
}

func feedsUpdateCacheIfEmpty(db gorm.DB, articles []Article, redis redis.Client) {
	r := feedsDefaultIndexQuery(db, articles)
	j, _ := json.Marshal(&r.Value)
	redis.Set("articles", j, cacheTime*time.Minute)
}

func feedsDefaultIndexQuery(db gorm.DB, articles []Article) *gorm.DB {
	return db.Preload("Source").Order("random()").Limit(500).Find(&articles)
}

func main() {

	redis := redisClient()
	feedsEmptyCache(redis)

	db, _ := gorm.Open("sqlite3", "feedme.db")
	db.LogMode(false)
	defer db.Close()

	var count int
	articles := []Article{}
	db.Find(&Article{}).Count(&count)
	if int(count) == 0 {
		feedsFirstImport(*db, articles, redis)
	}

	x, _ := redis.Get("articles").Result()
	if len(x) == 0 {
		feedsUpdateCacheIfEmpty(*db, articles, redis)
	}

	articleChron := time.Tick(1 * time.Hour)
	go func() {
		for now := range articleChron {
			log.Printf("Scheduled import started", now)
			updateSources()
			crawlAndParseArticles()
			log.Printf("Scheduled import finished")
		}
	}()

	redisChron := time.Tick(10 * time.Minute)
	go func() {
		for now := range redisChron {
			log.Printf("Cache update %v", now)
			r := feedsDefaultIndexQuery(*db, articles)
			j, _ := json.Marshal(&r.Value)
			redis.Set("articles", j, cacheTime*time.Minute)
		}
	}()

	router := httprouter.New()
	router.GET("/api/v1/articles", apiArticlesAll)
	router.GET("/api/v1/articles/:period", apiArticlesPeriod)
	router.GET("/", index)
	router.ServeFiles("/assets/*filepath", http.Dir("assets/"))

	log.Fatal(http.ListenAndServe(":8080", router))
}
