DROP TABLE ble_sessions;
DROP TABLE payment_authorizations;
DROP TABLE payment_intents;

ALTER TABLE wallets DROP CONSTRAINT wallets_user_id_currency_key;
