package zookeeper

import (
	"Zookeeper/internal/broker"
	"Zookeeper/internal/types"
	"database/sql"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Zookeeper struct {
	gin     *gin.Engine
	db      *sql.DB
	brokers map[string]*broker.Client
	replica int
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
		gin:     gin.Default(),
		db:      db,
		replica: viper.GetInt("replica"),
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

// Push pushes a message to the key
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
		err := s.AssignKey(elem.Key)
		if err != nil {
			log.WithFields(log.Fields{
				"key": elem.Key,
			}).Warn("Couldn't assign key to a broker: %s", err.Error())
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
			}).Warnf("Couldn't push message to broker: %s", err.Error())
			s.handle_down_broker(b)
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
		log.WithFields(log.Fields{
			"broker": b.Name,
		}).Info("Getting front value from broker")

		res, err = b.Front()
		if err != nil {
			log.WithFields(log.Fields{
				"broker": b.Name,
			}).Warnf("Couldn't get front value: %s", err.Error())
			s.handle_down_broker(b)
			continue
		}
		if res.Key == "" {
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
	}).Info("Popped message from key")
	c.JSON(200, gin.H{"message": "ok", "key": res.Key, "value": res.Value})
}

// Erase remove a message from queueName. It should be called after a message is popped from queueName
func (s *Zookeeper) Erase(key string) {
	log.WithFields(log.Fields{
		"key": key,
	}).Info("Erasing one message of a key from master and replica brokers")
	replicas := s.GetBrokers(key)
	for _, b := range replicas {
		err := b.Remove(key)
		if err != nil {
			log.WithFields(log.Fields{
				"key":    key,
				"broker": b.Name,
			}).Warnf("Couldn't remove message from broker: %s", err.Error())
		}
	}
}

func (s *Zookeeper) handle_down_broker(b *broker.Client) {
	err := b.HealthCheck()
	if err != nil {
		// get all keys assigned to the broker ans isMaster = True
		rows, err := s.db.Query("SELECT * FROM queues WHERE broker = $1 AND is_master = True", b.Name)

		if err != nil {
			log.WithFields(log.Fields{
				"broker": b.Name,
			}).Warnf("Couldn't get keys from database: %s", err.Error())
			return
		}

		defer func(rows *sql.Rows) {
			err := rows.Close()
			if err != nil {
				log.WithFields(log.Fields{
					"broker": b.Name,
				}).Warnf("Couldn't close rows: %s", err.Error())
			}
		}(rows)

		for rows.Next() {
			var key string
			var brokerName string
			var isMaster bool
			err := rows.Scan(&key, &isMaster, &brokerName)
			if err != nil {
				log.WithFields(log.Fields{
					"key":       key,
					"broker":    brokerName,
					"is_master": isMaster,
				}).Warnf("Couldn't scan row: %s", err.Error())
				continue
			}

			if !isMaster {
				continue
			} else {
				replicas := s.GetReplicaBrokers(key)
				if len(replicas) == 0 {
					log.WithFields(log.Fields{
						"key": key,
					}).Warn("No replica brokers found for key")
					continue
				}
				// set one replica as master
				err := s.db.QueryRow("UPDATE queues SET is_master = True WHERE queue = $1 AND broker = $2 RETURNING queue", key, replicas[0].Name).Scan(&key)
				if err != nil {
					log.WithFields(log.Fields{
						"key":    key,
						"broker": replicas[0].Name,
					}).Warnf("Couldn't set replica as master: %s", err.Error())
				}

				log.WithFields(log.Fields{
					"key":    key,
					"broker": replicas[0].Name,
				}).Info("Set replica as master")
			}
		}
		// delete broker from map and database
		delete(s.brokers, b.Name)
		_, err = s.db.Exec("DELETE FROM brokers WHERE name = $1", b.Name)
		if err != nil {
			log.WithFields(log.Fields{
				"broker": b.Name,
			}).Warnf("Couldn't delete broker from database: %s", err.Error())
		}

		log.WithFields(log.Fields{
			"broker": b.Name,
		}).Info("Deleted broker from map and database")
	}
}

func (s *Zookeeper) get_key_contents(key string) ([][]byte, error) {
	log.WithFields(log.Fields{
		"key": key,
	}).Info("Get key contents")

	// get brokers responsible for the key
	rows, err := s.db.Query("SELECT * FROM queues WHERE queue = $1", key)
	if err != nil {
		log.WithFields(log.Fields{
			"key": key,
		}).Warnf("Couldn't get brokers for key: %s", err.Error())
		return nil, err
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.WithFields(log.Fields{
				"key": key,
			}).Warnf("Couldn't close rows: %s", err.Error())
		}
	}(rows)

	for rows.Next() {
		var key string
		var brokerName string
		var isMaster bool
		err = rows.Scan(&key, &isMaster, &brokerName)
		if err != nil {
			log.WithFields(log.Fields{
				"key":       key,
				"broker":    brokerName,
				"is_master": isMaster,
			}).Warn(err.Error())
			return nil, err
		}
		// get broker
		b := s.brokers[brokerName]
		// get contents of the key
		contents, err := b.Export(key)
		if err != nil {
			log.WithFields(log.Fields{
				"key":    key,
				"broker": brokerName,
			}).Warnf("Couldn't get contents of key: %s", err.Error())
			return nil, err
		}
		return contents.Values, nil
	}
	//error
	return nil, errors.New("No broker found for key")
}
