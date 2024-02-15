package zookeeper

import (
	"Zookeeper/internal/broker"
	"Zookeeper/internal/types"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/zsais/go-gin-prometheus"
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
		go gs.BrokerHealthChecker(gs.brokers[b.Name])
		log.WithFields(log.Fields{
			"broker": b.Name,
			"host":   b.Host,
		}).Info("Registered broker successfully")
	}
	go gs.LoadBalancer()

	p := ginprometheus.NewPrometheus("gin")
	p.Use(gs.gin)

	gs.registerRoutes()
	return gs
}

func (s *Zookeeper) registerRoutes() {
	s.gin.POST("/pop", s.Pop)
	s.gin.POST("/push", s.Push)

	healthCheckURL := viper.GetString("health_check_path")
	s.gin.GET(healthCheckURL, s.healthCheck)
}

// Run runs the Zookeeper server
func (s *Zookeeper) Run() {
	err := s.gin.Run("0.0.0.0:" + viper.GetString("port"))
	if err != nil {
		log.Fatal(err.Error())
	}
}

func (s *Zookeeper) GetFastestBroker() string {
	var fastestBroker string
	var latency time.Duration
	for name, b := range s.brokers {
		if !b.Health {
			continue
		}
		if latency == 0 || b.Latency < latency {
			latency = b.Latency
			fastestBroker = name
		}
	}
	return fastestBroker
}

func (s *Zookeeper) GetSlowestBroker() string {
	var slowestBroker string
	var latency time.Duration
	for name, b := range s.brokers {
		if !b.Health {
			continue
		}
		if latency == 0 || b.Latency > latency {
			latency = b.Latency
			slowestBroker = name
		}
	}
	return slowestBroker
}

func (s *Zookeeper) ImportExport(source, target *broker.Client) {
	source.Mutex.Lock()
	target.Mutex.Lock()
	defer source.Mutex.Unlock()
	defer target.Mutex.Unlock()

	log.WithFields(log.Fields{
		"fastest_broker": target,
		"slowest_broker": source,
	}).Info("Scaling the slowest broker to the fastest broker")
	key, isMaster := s.GetRandomKey(source, target)
	if key == "" {
		log.Errorf("No keys found in slowest broker")
		return
	}
	keyData, err := source.Export(key)
	if err != nil {
		log.WithFields(log.Fields{
			"broker": source.Name,
			"key":    key,
		}).Errorf("Couldn't export key: %s", err.Error())
		return
	}

	log.WithFields(log.Fields{
		"broker":   target.Name,
		"key":      key,
		"isMaster": isMaster,
		"keyData":  keyData.Values,
	}).Info("Importing key to fastest broker")

	err = target.Import(key, isMaster, keyData.Values)
	if err != nil {
		log.WithFields(log.Fields{
			"broker": target.Name,
			"key":    key,
		}).Errorf("Couldn't import key: %s", err.Error())
		return
	}

	_, err = s.db.Exec("UPDATE queues SET broker = $1 WHERE broker = $2 AND queue = $3", target.Name, source.Name, key)
	if err != nil {
		log.WithFields(log.Fields{
			"broker": source.Name,
		}).Errorf("Couldn't update keys in database: %s", err.Error())
		return
	}
	log.WithFields(log.Fields{
		"broker": source.Name,
	}).Info("ImportExport done successfully")
}

func (s *Zookeeper) LoadBalancer() {
	d := viper.GetDuration("auto_scaling_interval")
	scaleFactor := viper.GetInt("scale_factor")
	ticker := time.NewTicker(d)

	for {
		select {
		case <-ticker.C:
			log.WithFields(log.Fields{
				"scale_factor": scaleFactor,
			}).Info("Checking if scaling is needed...")
			fastestBroker := s.brokers[s.GetFastestBroker()]
			slowestBroker := s.brokers[s.GetSlowestBroker()]
			//fastestBroker := s.brokers["broker1"]
			//slowestBroker := s.brokers["broker2"]

			if fastestBroker == nil || slowestBroker == nil {
				continue
			}
			if fastestBroker.Name == slowestBroker.Name {
				continue
			}
			if fastestBroker.Latency*time.Duration(scaleFactor) > slowestBroker.Latency {
				continue
			}

			s.ImportExport(slowestBroker, fastestBroker)
		}
	}
}

func (s *Zookeeper) GetRandomKey(slow *broker.Client, fast *broker.Client) (string, bool) {
	rows, err := s.db.Query("SELECT queue, is_master, broker FROM queues WHERE broker = $1 AND NOT EXISTS (SELECT * FROM queues q2 WHERE q2.broker = $2 AND q2.queue = queues.queue)", slow.Name, fast.Name)
	if err != nil {
		log.WithFields(log.Fields{
			"broker": slow.Name,
		}).Errorf("Couldn't get keys assigned to the queue from database: %s", err.Error())
		return "", false
	}
	for rows.Next() {
		var key string
		var isMaster bool
		var brokerName string
		err := rows.Scan(&key, &isMaster, &brokerName)
		if err != nil {
			log.WithFields(log.Fields{
				"broker": slow.Name,
			}).Warnf("Couldn't scan row: %s", err.Error())
			continue
		}
		return key, isMaster
	}
	return "", false
}

func (s *Zookeeper) BrokerHealthChecker(b *broker.Client) {
	d := viper.GetDuration("broker_health_check_interval")
	ticker := time.NewTicker(d)

	for {
		select {
		case <-ticker.C:
			err := b.HealthCheck()
			log.WithFields(log.Fields{
				"broker":  b.Name,
				"latency": b.Latency,
				"health":  b.Health,
				"err":     err,
			}).Info("Broker health checked successfully")
			if err == nil && b.Health {
				continue
			}
			if err != nil && !b.Health {
				continue
			}
			if err == nil && !b.Health {
				log.WithFields(log.Fields{
					"broker": b.Name,
				}).Info("Broker is up once again")
				b.Health = true
			} else {
				log.WithFields(log.Fields{
					"broker": b.Name,
				}).Warn("Broker is down.")
				err := s.RecoverFromFailure(b)
				if err != nil {
					log.WithFields(log.Fields{
						"broker": b.Name,
					}).Error("Couldn't recover from broker failure: %s", err.Error())
				} else {
					b.Health = false
				}
			}
		}
	}
}

func (s *Zookeeper) RecoverFromFailure(b *broker.Client) error {
	log.WithFields(log.Fields{
		"broker": b.Name,
	}).Info("Recovering from broker failure")
	rows, err := s.db.Query("SELECT * FROM queues WHERE broker = $1 AND is_master = True", b.Name)
	if err != nil {
		log.WithFields(log.Fields{
			"broker": b.Name,
		}).Warnf("Couldn't get keys assigned to the queue from database: %s", err.Error())
		return err
	}
	for rows.Next() {
		var key string
		var brokerName string
		var isMaster bool
		err := rows.Scan(&key, &isMaster, &brokerName)
		if err != nil {
			log.WithFields(log.Fields{
				"broker": b.Name,
			}).Warnf("Couldn't scan row: %s", err.Error())
			return err
		}
		if !isMaster {
			log.WithFields(log.Fields{
				"key": key,
			}).Warnf("This should not happen logically.")
			continue
		}
		replicas := s.GetReplicaBrokers(key)
		if len(replicas) == 0 {
			log.WithFields(log.Fields{
				"key": key,
			}).Warn("No replica brokers found for key")
			return errors.New("no replica brokers found for key")
		}
		selectedReplica := replicas[0]

		log.WithFields(log.Fields{
			"key":    key,
			"broker": selectedReplica.Name,
		}).Info("Setting replica as master")

		err = s.db.QueryRow("UPDATE queues SET is_master = True WHERE queue = $1 AND broker = $2 RETURNING queue", key, selectedReplica.Name).Scan(&key)
		if err != nil {
			log.WithFields(log.Fields{
				"key":    key,
				"broker": selectedReplica.Name,
			}).Warnf("Couldn't set replica as master in database: %s", err.Error())
			return err
		}
		err = selectedReplica.KeySetMaster(key, true)
		if err != nil {
			log.WithFields(log.Fields{
				"key":    key,
				"broker": selectedReplica.Name,
			}).Warnf("Couldn't set replica as master: %s", err.Error())
			return err
		}
		log.WithFields(log.Fields{
			"key":    key,
			"broker": selectedReplica.Name,
		}).Info("Set replica as master")
	}
	_, err = s.db.Exec("DELETE FROM queues WHERE broker = $1", b.Name)
	if err != nil {
		log.WithFields(log.Fields{
			"broker": b.Name,
		}).Warnf("Couldn't delete broker from database: %s", err.Error())
		return err
	}
	return nil
}

func (s *Zookeeper) healthCheck(c *gin.Context) {
	err := s.db.Ping()
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"message": err})
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
		log.WithFields(log.Fields{
			"key":    elem.Key,
			"broker": b.Name,
		}).Info("Pushing message to broker")
		err := b.Push(elem)
		if err != nil {
			log.WithFields(log.Fields{
				"key":    elem.Key,
				"broker": b.Name,
			}).Warnf("Couldn't push message to broker: %s", err.Error())
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
		if !b.Health {
			continue
		}
		var err error
		log.WithFields(log.Fields{
			"broker": b.Name,
		}).Info("Getting front value from broker")

		res, err = b.Front()
		if err != nil {
			log.WithFields(log.Fields{
				"broker": b.Name,
			}).Warnf("Couldn't get front value: %s", err.Error())
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
	}).Info("Erasing one message of a key replica brokers")
	replicas := s.GetReplicaBrokers(key)
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
