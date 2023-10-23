package handlers

import (
	"github.com/cockroachdb/pebble"
)

var DB *pebble.DB

func InitDB() {
	db, err := pebble.Open("database", &pebble.Options{})

	if err != nil {
		panic(err)
	}
	DB = db
}
