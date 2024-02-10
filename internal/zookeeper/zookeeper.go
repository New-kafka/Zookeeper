package zookeeper

import (
	"Zookeeper/internal/broker"
	"Zookeeper/internal/types"
	"database/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
	"log"
	"net/http"
)

type Zookeeper struct {
	gin     *gin.Engine
	db      *sql.DB
	brokers map[string]*broker.Client
}

// NewZookeeper returns a new Zookeeper instance
//
// TODO: read database connection info from a config file
func NewZookeeper() *Zookeeper {
	type postgresConfig struct {
		Host     string `yaml:"host"`
		Port     string `yaml:"port"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		Dbname   string `yaml:"dbname"`
	}
	var postgres postgresConfig
	if err := viper.UnmarshalKey("postgres", &postgres); err != nil {
		log.Fatal(err)
	}
	conninfo := "host=" + postgres.Host + " port=" + postgres.Port + " user=" + postgres.User + " password=" + postgres.Password + " dbname=" + postgres.Dbname + " sslmode=disable"
	db, err := sql.Open("postgres", conninfo)
	if err != nil {
		panic(err)
	}
	gs := &Zookeeper{
		gin: gin.Default(),
		db:  db,
	}
	gs.brokers = make(map[string]*broker.Client)
	type brokerConfig struct {
		Name string `yaml:"name"`
		Host string `yaml:"address"`
	}
	var brokers []brokerConfig
	// Unmarshal the brokers from the config file
	if err := viper.UnmarshalKey("brokers", &brokers); err != nil {
		log.Fatal(err)
	}
	log.Print("Registering brokers...")
	for _, b := range brokers {
		gs.brokers[b.Name] = broker.NewBroker(b.Name, b.Host)
		log.Printf("Registered broker \"%s\" in address \"%s\"", b.Name, b.Host)
	}
	gs.registerRoutes()
	return gs
}

func (s *Zookeeper) registerRoutes() {
	s.gin.POST("/pop", s.Pop)
	s.gin.POST("/push", s.Push)
}

// Run runs the Zookeeper server
func (s *Zookeeper) Run() {
	s.gin.Run("0.0.0.0:" + viper.GetString("port"))
}

// Push pushes a message to the queueName
func (s *Zookeeper) Push(c *gin.Context) {
	log.Print("Pushing message to queue")

	req := &types.PushRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	elem := &types.Element{
		QueueName: req.Key,
		Value:     req.Value,
	}
	log.Print("Getting master broker")
	if s.GetMasterBroker(elem.QueueName) == nil {
		log.Print("QueueName is not assigned to any broker yet. Assigning...")
		s.AssignQueue(elem.QueueName)
		log.Print("QueueName assigned successfully")
	}
	log.Printf("Pushing message to queue %s", elem.QueueName)

	brokers := s.GetBrokers(elem.QueueName)
	for _, b := range brokers {
		log.Printf("Pushing message to broker %s", b.Name)
		err := b.Push(elem)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
	return
}

// Pop pops a message
func (s *Zookeeper) Pop(c *gin.Context) {
	log.Print("Popping message from queue")
	var empty = true
	var res *types.Element
	for _, b := range s.brokers {
		var err error
		res, err = b.Front()
		if err == nil && res.QueueName != "" {
			log.Printf("Popping message from queue %s", res.QueueName)
			s.Erase(res.QueueName)
			empty = false
			log.Printf("Popped message from queue %s", res.QueueName)
			break
		}
	}

	log.Printf("Popped message from queue %s with value %s. Empty is %v", res.QueueName, res.Value, empty)
	if empty {
		c.JSON(200, gin.H{"message": "empty"})
		return
	}
	log.Printf("Popped message from queue %s with value %s", res.QueueName, res.Value)
	c.JSON(200, gin.H{"message": "ok", "value": res.Value})
}

// Erase remove a message from queueName. It should be called after a message is popped from queueName
func (s *Zookeeper) Erase(queueName string) {
	log.Printf("Removing message queue %s from all associated brokers", queueName)
	replicas := s.GetBrokers(queueName)
	for _, b := range replicas {
		log.Printf("Removing message %s from broker %s", queueName, b.Name)
		b.Remove(queueName)
	}
}
