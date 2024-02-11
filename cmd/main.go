package main

import (
	"Zookeeper/internal/zookeeper"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func init() {
	viper.SetConfigName("config")
	viper.AddConfigPath("./config")

	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}
}

func main() {
	log.SetLevel(log.DebugLevel)
	z := zookeeper.NewZookeeper()
	z.Run()
}
