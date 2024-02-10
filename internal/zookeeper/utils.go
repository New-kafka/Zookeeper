package zookeeper

import (
	"Zookeeper/internal/broker"
	"log"
	"math/rand"
)

// GetBrokers returns all brokers in the cluster, including the master and replicas
// which are responsible for the queueName
func (z *Zookeeper) GetBrokers(queueName string) []*broker.Client {
	master := z.GetMasterBroker(queueName)
	replicas := z.GetReplicaBrokers(queueName)
	if master != nil {
		replicas = append(replicas, master)
	}
	return replicas
}

// GetMasterBroker returns the master broker responsible for the queueName
func (z *Zookeeper) GetMasterBroker(queueName string) *broker.Client {
	rows, _ := z.db.Query("SELECT * FROM queues WHERE queue = $1 AND is_master = true", queueName) // TODO: handle logging errors
	defer rows.Close()
	log.Print("Getting master broker from database")
	for rows.Next() {
		var queue string
		var brokerName string
		var isMaster bool
		rows.Scan(&queue, &isMaster, &brokerName) // TODO: handle logging errors
		log.Printf("Master broker for queue %s is %s", queue, brokerName)
		if isMaster {
			return z.brokers[brokerName]
		}
	}
	return nil
}

// GetReplicaBrokers returns the replica brokers responsible for the queueName
func (z *Zookeeper) GetReplicaBrokers(queueName string) []*broker.Client {
	rows, _ := z.db.Query("SELECT * FROM queues WHERE queue = $1", queueName) // TODO: handle logging errors
	defer rows.Close()
	result := []*broker.Client{}
	for rows.Next() {
		var queue string
		var brokerName string
		var isMaster bool
		rows.Scan(&queue, &isMaster, &brokerName) // TODO: handle logging errors
		if !isMaster {
			result = append(result, z.brokers[brokerName])
		}
	}
	return result
}

// AssignQueue assigns the queueName to a random broker in the cluster as the master
// and assigns the queueName to the remaining brokers in the cluster as replicas
//
// Note: this only happens when the queueName is first created
//
// TODO: balance the load better than a simple random
//
// TODO: add a replica factor k and add queue to k brokers
func (z *Zookeeper) AssignQueue(queueName string) {
	log.Print("Assigning queue to brokers")

	randomIndex := rand.Intn(len(z.brokers))
	counter := 0
	for _, b := range z.brokers {
		isMaster := counter == randomIndex
		counter++
		err := b.AddQueue(queueName, isMaster)
		if err != nil {
			log.Print(err)
		}
		z.db.Exec("INSERT INTO queues (queue, broker, is_master) VALUES ($1, $2, $3)", queueName, b.Name, isMaster) // TODO: handle logging errors
	}
}
