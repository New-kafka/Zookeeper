package zookeeper

import (
	"Zookeeper/internal/broker"
	"database/sql"
	"time"

	log "github.com/sirupsen/logrus"
)

// GetBrokers returns all brokers in the cluster, including the master and replicas
// which are responsible for the key
func (z *Zookeeper) GetBrokers(key string) []*broker.Client {
	log.WithFields(log.Fields{
		"key": key,
	}).Info("Get brokers")

	master := z.GetMasterBroker(key)
	replicas := z.GetReplicaBrokers(key)
	if master != nil {
		replicas = append(replicas, master)
	}
	return replicas
}

// GetMasterBroker returns the master broker responsible for the key
func (z *Zookeeper) GetMasterBroker(key string) *broker.Client {
	log.WithFields(log.Fields{
		"key": key,
	}).Info("Get master broker")

	rows, err := z.db.Query("SELECT * FROM queues WHERE queue = $1 AND is_master = True", key)
	if err != nil {
		log.WithFields(log.Fields{
			"key": key,
		}).Warnf("Couldn't get master broker: %s", err.Error())
		return nil
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
			return nil
		}
		if isMaster {
			return z.brokers[brokerName]
		}
	}
	return nil
}

// GetReplicaBrokers returns the replica brokers responsible for the key
func (z *Zookeeper) GetReplicaBrokers(key string) []*broker.Client {
	log.WithFields(log.Fields{
		"key": key,
	}).Info("Get replica brokers")

	rows, err := z.db.Query("SELECT * FROM queues WHERE queue = $1 AND is_master = False", key)
	if err != nil {
		log.WithFields(log.Fields{
			"key": key,
		}).Warnf("Couldn't get replica brokers: %s", err.Error())
		return []*broker.Client{}
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.WithFields(log.Fields{
				"key": key,
			}).Warnf("Couldn't close rows: %s", err.Error())
		}
	}(rows)

	result := []*broker.Client{}
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
			}).Warn(err.Error())
		}
		if !isMaster {
			result = append(result, z.brokers[brokerName])
		}
	}
	return result
}

// AssignKey assigns the queueName to a random broker in the cluster as the master
// and assigns the queueName to the remaining brokers in the cluster as replicas
// TODO: balance the load better than a simple random
//
// TODO: add a replica factor k and add queue to k brokers
func (z *Zookeeper) AssignKey(key string) error {
	log.WithFields(log.Fields{
		"key": key,
	}).Info("Assign key to a broker")

	brokers := z.GetFreeBrokers(z.replica)

	for index, b := range brokers {
		var isMaster bool = false
		if index == 0 {
			isMaster = true
		}

		log.WithFields(log.Fields{
			"key":       key,
			"broker":    b.Name,
			"is_master": isMaster,
		}).Debug("Assign key to broker with details")

		err := b.AddKey(key, isMaster)
		if err != nil {
			log.WithFields(log.Fields{
				"key":       key,
				"broker":    b.Name,
				"is_master": isMaster,
			}).Warnf("Couldn't add key to broker upstream: %s", err.Error())
			return err
		}

		_, err = z.db.Exec("INSERT INTO queues (queue, broker, is_master) VALUES ($1, $2, $3)", key, b.Name, isMaster)
		if err != nil {
			log.WithFields(log.Fields{
				"key":       key,
				"broker":    b.Name,
				"is_master": isMaster,
			}).Warnf("Couldn't add key to database: %s", err.Error())
			return err
		}
	}
	return nil
}

func (z *Zookeeper) GetFreeBrokers(count int) []*broker.Client {
	log.WithFields(log.Fields{
		"count": count,
	}).Info("Get free brokers")

	type MinimumLatencyBroker struct {
		broker  *broker.Client
		latency time.Duration
	}
	var list []MinimumLatencyBroker

	for _, b := range z.brokers {
		start := time.Now()
		err := b.HealthCheck()
		if err != nil {
			log.WithFields(log.Fields{
				"broker": b.Name,
			}).Warnf("Broker is not healthy: %s", err.Error())
			continue
		}
		latency := time.Since(start)
		log.WithFields(log.Fields{
			"broker":  b.Name,
			"latency": latency,
		}).Debug("Broker is healthy")

		if len(list) < count {
			list = append(list, MinimumLatencyBroker{
				broker:  b,
				latency: latency,
			})
			continue
		}

		maximumIndex := -1
		maximumLatency := time.Duration(0)
		for index, item := range list {
			if item.latency < maximumLatency {
				maximumIndex = index
				maximumLatency = item.latency
			}
		}
		if maximumLatency > latency {
			list[maximumIndex] = MinimumLatencyBroker{
				broker:  b,
				latency: latency,
			}
		}
	}
	var res []*broker.Client
	for _, item := range list {
		log.WithFields(log.Fields{
			"broker": item.broker.Name,
		}).Info("Selected broker")
		res = append(res, item.broker)
	}
	return res
}
