package zookeeper

import (
	"Zookeeper/internal/broker"
	"database/sql"
	log "github.com/sirupsen/logrus"
	"math/rand"
)

// GetBrokers returns all brokers in the cluster, including the master and replicas
// which are responsible for the queueName
func (z *Zookeeper) GetBrokers(queueName string) []*broker.Client {
	log.WithFields(log.Fields{
		"key": queueName,
	}).Info("Get brokers")

	master := z.GetMasterBroker(queueName)
	replicas := z.GetReplicaBrokers(queueName)
	if master != nil {
		replicas = append(replicas, master)
	}
	return replicas
}

// GetMasterBroker returns the master broker responsible for the queueName
func (z *Zookeeper) GetMasterBroker(queueName string) *broker.Client {
	log.WithFields(log.Fields{
		"key": queueName,
	}).Info("Get master broker")

	rows, err := z.db.Query("SELECT * FROM queues WHERE queue = $1 AND is_master = True", queueName) // TODO: handle logging errors
	if err != nil {
		log.WithFields(log.Fields{
			"key": queueName,
		}).Warnf("Couldn't get master broker: %s", err.Error())
		return nil
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.WithFields(log.Fields{
				"key": queueName,
			}).Warnf("Couldn't close rows: %s", err.Error())
		}
	}(rows)

	for rows.Next() {
		var queue string
		var brokerName string
		var isMaster bool
		err = rows.Scan(&queue, &isMaster, &brokerName)
		if err != nil {
			log.WithFields(log.Fields{
				"key":       queue,
				"broker":    brokerName,
				"is_master": isMaster,
			}).Warn(err.Error())
			return nil
		}
		if isMaster {
			return z.brokers[brokerName]
		}
	}
	return nil
}

// GetReplicaBrokers returns the replica brokers responsible for the queueName
func (z *Zookeeper) GetReplicaBrokers(queueName string) []*broker.Client {
	log.WithFields(log.Fields{
		"key": queueName,
	}).Info("Get replica brokers")

	rows, err := z.db.Query("SELECT * FROM queues WHERE queue = $1 AND is_master = False", queueName)
	if err != nil {
		log.WithFields(log.Fields{
			"key": queueName,
		}).Warnf("Couldn't get replica brokers: %s", err.Error())
		return []*broker.Client{}
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.WithFields(log.Fields{
				"key": queueName,
			}).Warnf("Couldn't close rows: %s", err.Error())
		}
	}(rows)

	result := []*broker.Client{}
	for rows.Next() {
		var queue string
		var brokerName string
		var isMaster bool
		err := rows.Scan(&queue, &isMaster, &brokerName)
		if err != nil {
			log.WithFields(log.Fields{
				"key":       queue,
				"broker":    brokerName,
				"is_master": isMaster,
			}).Warn(err.Error())
		}
		if !isMaster {
			result = append(result, z.brokers[brokerName])
		}
	}
	return result
}

// AssignQueue assigns the queueName to a random broker in the cluster as the master
// and assigns the queueName to the remaining brokers in the cluster as replicas
// TODO: balance the load better than a simple random
//
// TODO: add a replica factor k and add queue to k brokers
func (z *Zookeeper) AssignQueue(queueName string) error {
	log.WithFields(log.Fields{
		"key": queueName,
	}).Info("Assign key to a broker")

	randomIndex := rand.Intn(len(z.brokers))
	counter := 0
	for _, b := range z.brokers {
		isMaster := counter == randomIndex
		counter++
		err := b.AddQueue(queueName, isMaster)
		if err != nil {
			log.WithFields(log.Fields{
				"key":       queueName,
				"broker":    b.Name,
				"is_master": isMaster,
			}).Warnf("Couldn't add key to broker upstream: %s", err.Error())
			return err
		}

		_, err = z.db.Exec("INSERT INTO queues (queue, broker, is_master) VALUES ($1, $2, $3)", queueName, b.Name, isMaster) // TODO: handle logging errors
		if err != nil {
			log.WithFields(log.Fields{
				"key":       queueName,
				"broker":    b.Name,
				"is_master": isMaster,
			}).Warnf("Couldn't add key to database: %s", err.Error())
			return err
		}
	}
	return nil
}
