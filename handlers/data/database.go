package handlers

import (
	"github.com/nutsdb/nutsdb"
)

var DB *nutsdb.DB

func InitDB() {
	db, err := nutsdb.Open(
		nutsdb.DefaultOptions,
		nutsdb.WithDir("database"),
		nutsdb.WithRWMode(nutsdb.MMap),
	)
	if err != nil {
		panic(err)
	}
	DB = db
}
