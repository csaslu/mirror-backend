CREATE TYPE mirror_type AS ENUM (
    'reverse_proxy', 'rsync',
    'http', 'https',
    'ftp', 's3'
);

CREATE TABLE mirror_list (
    id         SERIAL        PRIMARY KEY,
    key        TEXT          NOT NULL,
    comment    TEXT,
    type       mirror_type   NOT NULL,
    source     TEXT          NOT NULL
);
