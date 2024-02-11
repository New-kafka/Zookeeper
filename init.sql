CREATE TABLE queues (
    queue VARCHAR(255) NOT NULL,
    is_master BOOLEAN DEFAULT false,
    broker VARCHAR(255) NOT NULL,
    PRIMARY KEY (queue, broker)
);