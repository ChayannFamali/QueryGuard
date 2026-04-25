-- Тестовые таблицы для разработки

CREATE TABLE IF NOT EXISTS users (
    id          SERIAL PRIMARY KEY,
    name        VARCHAR(100) NOT NULL,
    email       VARCHAR(100) UNIQUE NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orders (
    id          SERIAL PRIMARY KEY,
    user_id     INTEGER REFERENCES users(id),
    amount      DECIMAL(10,2) NOT NULL,
    status      VARCHAR(20) DEFAULT 'pending',
    created_at  TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS products (
    id          SERIAL PRIMARY KEY,
    name        VARCHAR(100) NOT NULL,
    price       DECIMAL(10,2) NOT NULL,
    stock       INTEGER DEFAULT 0
);

-- Тестовые данные
INSERT INTO users (name, email)
SELECT
    'User ' || i,
    'user' || i || '@example.com'
FROM generate_series(1, 1000) AS i;

INSERT INTO products (name, price, stock)
SELECT
    'Product ' || i,
    (random() * 1000)::DECIMAL(10,2),
    (random() * 100)::INTEGER
FROM generate_series(1, 100) AS i;

INSERT INTO orders (user_id, amount, status)
SELECT
    (random() * 999 + 1)::INTEGER,
    (random() * 500)::DECIMAL(10,2),
    (ARRAY['pending', 'completed', 'cancelled'])[floor(random() * 3 + 1)]
FROM generate_series(1, 5000) AS i;
