DROP TABLE IF EXISTS word CASCADE;
CREATE TABLE word (
    id serial PRIMARY KEY,
    word varchar(256),
    part_of_speech varchar(64),
    transcription_UK varchar(256),
    transcription_US varchar(256)
);
DROP TABLE IF EXISTS definition CASCADE;
CREATE TABLE definition (
    id serial PRIMARY KEY,
    word_id serial,
    definition varchar(512),

    FOREIGN KEY(word_id)
    REFERENCES word(id)
);
DROP TABLE IF EXISTS example CASCADE;
CREATE TABLE example (
    id serial PRIMARY KEY,
    definition_id serial,
    example varchar(512),

    FOREIGN KEY(definition_id)
    REFERENCES definition(id)
);