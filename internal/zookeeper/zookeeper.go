package zookeeper

import (
	"Zookeeper/internal/broker"
	"Zookeeper/internal/types"
	"database/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"net/http"
)

type Zookeeper struct {
	gin     *gin.Engine
	db      *sql.DB
	brokers map[string]*broker.Client
}

// NewZookeeper returns a new Zookeeper instance
func NewZookeeper() *Zookeeper {
	type postgresConfig struct {
		Host     string `yaml:"host" binding:"required"`
		Port     string `yaml:"port" binding:"required"`
		User     string `yaml:"user" binding:"required"`
		Password string `yaml:"password" binding:"required"`
		Dbname   string `yaml:"dbname" binding:"required"`
	}
	var postgres postgresConfig
	if err := viper.UnmarshalKey("postgres", &postgres); err != nil {
		log.Error(err.Error())
	}

	conninfo := "host=" + postgres.Host + " port=" + postgres.Port + " user=" + postgres.User + " password=" + postgres.Password + " dbname=" + postgres.Dbname + " sslmode=disable"
	db, err := sql.Open("postgres", conninfo)
	if err != nil {
		log.Fatal(err.Error())
	}
	err = db.Ping()
	if err != nil {
		log.WithFields(log.Fields{
			"host":   postgres.Host,
			"port":   postgres.Port,
			"dbname": postgres.Dbname,
		}).Fatal(err.Error())
	}
	log.WithFields(log.Fields{
		"host":   postgres.Host,
		"port":   postgres.Port,
		"dbname": postgres.Dbname,
	}).Debugf("Connected to database successfully")

	gs := &Zookeeper{
		gin: gin.Default(),
		db:  db,
	}

	gs.brokers = make(map[string]*broker.Client)
	type brokerConfig struct {
		Name string `yaml:"name" binding:"required"`
		Host string `yaml:"address" binding:"required"`
	}
	var brokers []brokerConfig
	if err := viper.UnmarshalKey("brokers", &brokers); err != nil {
		log.Fatal(err)
	}

	for _, b := range brokers {
		gs.brokers[b.Name] = broker.NewBroker(b.Name, b.Host)
		log.WithFields(log.Fields{
			"broker": b.Name,
			"host":   b.Host,
		}).Info("Registered broker successfully")
	}
	gs.registerRoutes()
	return gs
}

func (s *Zookeeper) registerRoutes() {
	s.gin.POST("/pop", s.Pop)
	s.gin.POST("/push", s.Push)

	healthCheck := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	}
	healthCheckURL := viper.GetString("health_check_path")
	s.gin.GET(healthCheckURL, healthCheck)
}

// Run runs the Zookeeper server
func (s *Zookeeper) Run() {
	err := s.gin.Run("0.0.0.0:" + viper.GetString("port"))
	if err != nil {
		log.Fatal(err.Error())
	}
}

// Push pushes a message to the queueName
func (s *Zookeeper) Push(c *gin.Context) {
	elem := &types.Element{}
	if err := c.ShouldBindJSON(&elem); err != nil {
		log.Debugf("Error binding request: %s", err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if s.GetMasterBroker(elem.Key) == nil {
		log.WithFields(log.Fields{
			"key": elem.Key,
		}).Infof("No master broker found for key. Assigning one...")
		err := s.AssignQueue(elem.Key)
		if err != nil {
			log.WithFields(log.Fields{
				"key": elem.Key,
			}).Info("Couldn't assign key to a broker: %s", err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	brokers := s.GetBrokers(elem.Key)
	for _, b := range brokers {
		err := b.Push(elem)
		if err != nil {
			log.WithFields(log.Fields{
				"key":    elem.Key,
				"broker": b.Name,
			}).Info("Couldn't push message to broker: %s", err.Error())
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
	return
}

// Pop pops a message
func (s *Zookeeper) Pop(c *gin.Context) {
	var empty = true
	var res *types.Element
	for _, b := range s.brokers {
		var err error
		res, err = b.Front()
		if err != nil {
			log.WithFields(log.Fields{
				"broker": b.Name,
			}).Warnf("Couldn't get front value: %s", err.Error())
			continue
		}
		if res == nil {
			continue
		}
		log.WithFields(log.Fields{
			"broker": b.Name,
			"key":    res.Key,
		}).Info("Got a message from broker")
		s.Erase(res.Key)
		empty = false
		break
	}

	if empty {
		log.Info("Queue is empty")
		c.JSON(200, gin.H{"message": "Queue is empty"})
		return
	}
	log.WithFields(log.Fields{
		"key":   res.Key,
		"value": res.Value,
	}).Info("Popped message from queue")
	c.JSON(200, gin.H{"message": "ok", "key": res.Key, "value": res.Value})
}

// Erase remove a message from queueName. It should be called after a message is popped from queueName
func (s *Zookeeper) Erase(queueName string) {
	log.WithFields(log.Fields{
		"key": queueName,
	}).Info("Erasing one message of a key from master and replica brokers")
	replicas := s.GetBrokers(queueName)
	for _, b := range replicas {
		err := b.Remove(queueName)
		if err != nil {
			log.WithFields(log.Fields{
				"key":    queueName,
				"broker": b.Name,
			}).Warnf("Couldn't remove message from broker: %s", err.Error())
		}
	}
}
