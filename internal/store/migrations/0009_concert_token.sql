-- Token of the public link /c/<token>; nullable as ADD COLUMN refuses a random default (CreateConcert sets it).
ALTER TABLE concerts ADD COLUMN token TEXT;
UPDATE concerts SET token = lower(hex(randomblob(12)));
CREATE UNIQUE INDEX concerts_token ON concerts (token);
