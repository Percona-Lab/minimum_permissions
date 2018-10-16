# MySQL Minimum Permissions tool
Get the minimum set of permissions needed to run a particular query
Many times at Percona we found that a big number of MySQL instances running with users having too many permissions they
don't need. In general, many persons just do `GRANT ALL on *.* to 'user'@'host'`.
This is a big security risk not only to prevent external/unauthorized access to the database, but also lets that user to
any any query, even those queries that deletes data o alter the database structure.
  
This tool born when we were testing Percona Toolkit with MySQL 8.0.4-rc to prepare all the tools to run under the new MySQL
version and we came across this scenario:

One of pt-table-sync's test, needs to check the output when running with an underprivileged user.
To do that, it creates a user and grants him these permissions:

```
GRANT SUPER, SELECT, UPDATE, SHOW DATABASES ON *.* TO 'test_907'\@'localhost' IDENTIFIED BY 'msandbox'
```
and then, pt-table-checksum runs this query:

```
SELECT `i`, COUNT(*) AS test_count FROM `issue_907`.`t` WHERE 1=1 GROUP BY i ORDER BY i LOCK IN SHARE MODE
```
The problem here is that granting `UPDATE` and `SHOW DATABASES` is enough on MySQL 5.7 but it isn’t for MySQL 8.0.4-rc so, which 
are the minimum permissions we need?

## How it works?

The tool builds a list of all possible all permissions and then creates a testing user granting him permissions individually, in groups of 2, in groups of 3, etc, using all combinations and runs the provided queries until it found the query execution was
successful, grouping queries with their minimum required grants.
  
####  Example:
 Suppose we are trying to get the minimum permissions for this query: `SHOW /*!40100 ENGINE*/ INNODB STATUS`.  
 The program will start the sandbox, and it will create a testing user granting him  `SELECT` permission and it will run the query. If the query execution fails, it will grant `INSERT` to the testing user and so on until, for this particular example, when the testing user has been granted with `SELECT, PROCESS` the query execution will succed and we know that `SELECT, PROCESS` are the minimum permissions required to run the query.
 
### When a query execution was successful?
Since the program runs in a MySQL sandbox, most queries will fail. For example, if we try to execute a `SELECT field1 FROM foo.bar`, the `foo` database and the `bar` table won't exists but, if while trying to run the query we got one of these errors, it means that at least, the testing user has been granted with the minimum permissions requiered to run the query:

|Error Code|Meaning|
|-----|-----|
|1049|Database doesn't exists|
|1067|Invalid default value for '%s'|
|1146|Table doesn't exists|
|1213|Deadlock found when trying to get lock|
|1215|Cannot add FK constraint|
|1231|Invalid value for variable|
|1049|Unknown database '%s'|
|1146|Table '%s.%s' doesn't exist|
|1213|Deadlock found when trying to get lock|
|1215|Cannot add foreign key constraint|
|1231|Variable '%s' can't be set to the value of '%s'|

## Why do we need a MySQL sandbox?
The program will start its own MySQL instance (sandbox) because it is dangerous to run a query on an existing database. Even if we try to enclose the queries in a transaction, there are statements that have implicit autocommit.  
See MySQL reference: [13.3.3 Statements That Cause an Implicit Commit](https://dev.mysql.com/doc/refman/8.0/en/implicit-commit.html)

## Usage examples
Since this program runs queries that could modify, alter or delete data, it cannot be ran using an existing MySQL
instance for security reasons. Because of that, the program needs to know the location of the MySQL binaries and it will
start its own MySQL sandbox instance in a temporary directory.

Lets assume we have these directories having different MySQL flavors and versions, at `~/mysql`:  
```
├── mdb-10.1
├── mdb-10.2
├── my-5.5
├── my-5.6
├── my-5.7
├── my-8.0
├── ps-5.5
├── ps-5.6
└── ps-5.7
```
#### Testing all queries from a slow.log file
```
./minimum_permissions --mysql-base-dir=~/mysql/my-8.0 --slow-log=~/slow.log

```

#### Testing individual queries
```
./minimum_permissions --mysql-base-dir=~/mysql/my-8.0 -q='SELECT f1 FROM foo.bar' -q='SELECT f2 FROM db1.t1'

```

#### Testing queries from multiple sources at the same time
```
./minimum_permissions --mysql-base-dir=~/mysql/my-8.0 -q='SELECT f1 FROM foo.bar' -q='SELECT f2 FROM db1.t1' --slow-log=~/slow.log --input-file=~/queries.txt --gen-log=~/genlog

```
### Flags
|Flag|Description|Notes|
|-----|-----|-----|
|--debug|Show extra debug information|default: false |
|-g, --gen-log|Load queries from genlog file|
|-h, --help|Show context-sensitive help (also try --help-long and --help-man)| |
|--hide-invalid-queries|Do not include invalid queries in the report|Default: false|
|-i, --input-file|Load queries from plain text file. Queries in this file must end with a ; and can have multiple lines| |
|--keep-sandbox|Do not stop/remove the sandbox after finishing|Default: false|
|--max-depth|Maximum number of simultaneous permissions to try|Default: 10|
|--mysql-base-dir|Path to the MySQL base directory (parent of bin/)|Required|
|--no-trim-long-queries|Do not trim long queries|Default: false|
|-q, --query|Individual query to test. Can be specified multiple times| |
|--quiet|Don't show info level notificacions and progress|Default: false|
|-s, --slow-log|Load queries from slow log file| |
|--trim-query-size|Trim queries longer than trim-query-size|Default: 100|
|--version|Show version and exit| |

## Output
The output will be something like this:
```
### Minimum Permissions

----------------------------------------------------------------------------------------------------
Grants : ALTER
----------------------------------------------------------------------------------------------------
ALTER TABLE `film_text` DISABLE KEYS
ALTER TABLE `country` DISABLE KEYS
ALTER TABLE `rental` ENABLE KEYS
ALTER TABLE `customer` ENABLE KEYS
ALTER TABLE `actor` ENABLE KEYS
ALTER TABLE `staff` DISABLE KEYS
ALTER TABLE `film` DISABLE KEYS
ALTER TABLE `language` DISABLE KEYS
ALTER TABLE `actor` DISABLE KEYS
ALTER TABLE `country` ENABLE KEYS
ALTER TABLE `payment` DISABLE KEYS
ALTER TABLE `inventory` DISABLE KEYS
ALTER TABLE `customer` DISABLE KEYS
ALTER TABLE `rental` DISABLE KEYS
ALTER TABLE `store` ENABLE KEYS
ALTER TABLE `film_category` DISABLE KEYS
ALTER TABLE `store` DISABLE KEYS
ALTER TABLE `category` DISABLE KEYS
ALTER TABLE `payment` ENABLE KEYS
ALTER TABLE `film_actor` ENABLE KEYS
ALTER TABLE `film` ENABLE KEYS
ALTER TABLE `film_actor` DISABLE KEYS
ALTER TABLE `address` DISABLE KEYS
ALTER TABLE `film_category` ENABLE KEYS
ALTER TABLE `category` ENABLE KEYS
ALTER TABLE `city` ENABLE KEYS
ALTER TABLE `city` DISABLE KEYS
ALTER TABLE `language` ENABLE KEYS
ALTER TABLE `film_text` ENABLE KEYS
ALTER TABLE `inventory` ENABLE KEYS
ALTER TABLE `address` ENABLE KEYS
ALTER TABLE `staff` ENABLE KEYS 


----------------------------------------------------------------------------------------------------
Grants : CREATE
----------------------------------------------------------------------------------------------------
CREATE TABLE `ndb_binlog_index` (
  `Position` bigint(20) unsigned NOT NULL,
  `File` varchar(255) N ... (truncated)
CREATE TABLE `country` (
  `country_id` smallint(5) unsigned NOT NULL AUTO_INCREMENT,
  `country` va ... (truncated)
CREATE TABLE `proxies_priv` (
  `Host` char(60) COLLATE utf8_bin NOT NULL DEFAULT '',
  `User` char( ... (truncated)
CREATE TABLE IF NOT EXISTS `user` (
  `Host` char(60) COLLATE utf8_bin NOT NULL DEFAULT '',
  `User` ... (truncated)
CREATE TABLE IF NOT EXISTS percona_test.load_data (i int)
CREATE DATABASE IF NOT EXISTS percona_test
CREATE TABLE percona_test.checksums(
          db_tbl varchar(128) not null primary key,
          c ... (truncated)
CREATE TABLE `tables_priv` (
  `Host` char(60) COLLATE utf8_bin NOT NULL DEFAULT '',
  `Db` char(64) ... (truncated)
CREATE TABLE IF NOT EXISTS `general_log` (
  `event_time` timestamp(6) NOT NULL DEFAULT CURRENT_TIME ... (truncated)
CREATE TABLE `db` (
  `Host` char(60) COLLATE utf8_bin NOT NULL DEFAULT '',
  `Db` char(64) COLLATE  ... (truncated)
CREATE TABLE `language` (
  `language_id` tinyint(3) unsigned NOT NULL AUTO_INCREMENT,
  `name` char ... (truncated)
CREATE TABLE `func` (
  `name` char(64) COLLATE utf8_bin NOT NULL DEFAULT '',
  `ret` tinyint(1) NOT ... (truncated)
CREATE TABLE `actor` (
  `actor_id` smallint(5) unsigned NOT NULL AUTO_INCREMENT,
  `first_name` var ... (truncated)
CREATE TABLE `category` (
  `category_id` tinyint(3) unsigned NOT NULL AUTO_INCREMENT,
  `name` varc ... (truncated)
CREATE TABLE `columns_priv` (
  `Host` char(60) COLLATE utf8_bin NOT NULL DEFAULT '',
  `Db` char(64 ... (truncated)
CREATE TABLE `film_text` (
  `film_id` smallint(6) NOT NULL,
  `title` varchar(255) NOT NULL,
  `des ... (truncated)
CREATE DATABASE `sakila`
CREATE TABLE IF NOT EXISTS percona_test.sentinel (id INT PRIMARY KEY, ping VARCHAR(64) NOT NULL DEFA ... (truncated)
CREATE TABLE `proc` (
  `db` char(64) CHARACTER SET utf8 COLLATE utf8_bin NOT NULL DEFAULT '',
  `na ... (truncated)
CREATE TABLE `event` (
  `db` char(64) CHARACTER SET utf8 COLLATE utf8_bin NOT NULL DEFAULT '',
  `n ... (truncated)
CREATE TABLE `procs_priv` (
  `Host` char(60) COLLATE utf8_bin NOT NULL DEFAULT '',
  `Db` char(64)  ... (truncated)
CREATE TABLE IF NOT EXISTS `slow_log` (
  `start_time` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTA ... (truncated) 


----------------------------------------------------------------------------------------------------
Grants : CREATE, REFERENCES
----------------------------------------------------------------------------------------------------
CREATE TABLE `city` (
  `city_id` smallint(5) unsigned NOT NULL AUTO_INCREMENT,
  `city` varchar(50) ... (truncated)
CREATE TABLE `staff` (
  `staff_id` tinyint(3) unsigned NOT NULL AUTO_INCREMENT,
  `first_name` varc ... (truncated)
CREATE TABLE `address` (
  `address_id` smallint(5) unsigned NOT NULL AUTO_INCREMENT,
  `address` va ... (truncated)
CREATE TABLE `store` (
  `store_id` tinyint(3) unsigned NOT NULL AUTO_INCREMENT,
  `manager_staff_id ... (truncated)
CREATE TABLE `payment` (
  `payment_id` smallint(5) unsigned NOT NULL AUTO_INCREMENT,
  `customer_id ... (truncated)
CREATE TABLE `rental` (
  `rental_id` int(11) NOT NULL AUTO_INCREMENT,
  `rental_date` datetime NOT  ... (truncated)
CREATE TABLE `customer` (
  `customer_id` smallint(5) unsigned NOT NULL AUTO_INCREMENT,
  `store_id` ... (truncated)
CREATE TABLE `inventory` (
  `inventory_id` mediumint(8) unsigned NOT NULL AUTO_INCREMENT,
  `film_i ... (truncated)
CREATE TABLE `film_actor` (
  `actor_id` smallint(5) unsigned NOT NULL,
  `film_id` smallint(5) unsi ... (truncated)
CREATE TABLE `film` (
  `film_id` smallint(5) unsigned NOT NULL AUTO_INCREMENT,
  `title` varchar(25 ... (truncated)
CREATE TABLE `film_category` (
  `film_id` smallint(5) unsigned NOT NULL,
  `category_id` tinyint(3) ... (truncated) 


----------------------------------------------------------------------------------------------------
Grants : DROP
----------------------------------------------------------------------------------------------------
DROP TABLE IF EXISTS `procs_priv`
DROP TABLE IF EXISTS percona_test.checksums
DROP TABLE IF EXISTS `func`
DROP TABLE IF EXISTS `tables_priv`
DROP TABLE IF EXISTS `ndb_binlog_index`
DROP TABLE IF EXISTS `proxies_priv`
DROP DATABASE IF EXISTS `sakila`
DROP TABLE IF EXISTS `db`
DROP TABLE IF EXISTS `event`
DROP TABLE IF EXISTS `proc`
DROP TABLE IF EXISTS `columns_priv` 


----------------------------------------------------------------------------------------------------
Grants : INSERT
----------------------------------------------------------------------------------------------------
INSERT INTO `category` VALUES (1,'Action','2006-02-15 11:46:27')
INSERT INTO `city` VALUES (1,'A Corua (La Corua)',87,'2006-02-15 11:45:25')
INSERT INTO `inventory` VALUES (1,1,1,'2006-02-15 12:09:17')
INSERT INTO `store` VALUES (1,1,1,'2006-02-15 11:57:12'),(2,2,2,'2006-02-15 11:57:12')
INSERT INTO `address` VALUES (1,'47 MySakila Drive',NULL,'Alberta',300,'','','2006-02-15 11:45:30')
INSERT INTO `film_actor` VALUES (1,1,'2006-02-15 12:05:03')
INSERT INTO `film_category` VALUES (1,6,'2006-02-15 12:07:09')
INSERT INTO `country` VALUES (1,'Afghanistan','2006-02-15 11:44:00')
INSERT INTO `payment` VALUES (14872,554,2,15690,'4.99','2005-08-23 09:53:30','2006-02-16 05:21:52')
INSERT INTO `film_text` VALUES (1,'ACADEMY DINOSAUR','A Epic Drama of a Feminist And a Mad Scientist ... (truncated)
INSERT INTO `language` VALUES (1,'English','2006-02-15 12:02:19')
INSERT INTO `rental` VALUES (12370,'2005-08-18 07:57:47',3343,503,'2005-08-22 11:32:47',1,'2006-02-1 ... (truncated)
INSERT INTO `actor` VALUES (1,'PENELOPE','GUINESS','2006-02-15 11:34:33')
INSERT INTO percona_test.checksums(db_tbl, checksum)
   VALUES('sakila.store', 972255196)
INSERT INTO `customer` VALUES (1,1,'MARY','SMITH','MARY.SMITH@sakilacustomer.org',5,1,'2006-02-14 22 ... (truncated)
INSERT INTO `staff` VALUES (1,'Mike','Hillyer',3,NULL)
INSERT INTO `film` VALUES (1,'ACADEMY DINOSAUR','A Epic Drama of a Feminist And a Mad Scientist who  ... (truncated) 


----------------------------------------------------------------------------------------------------
Grants : INSERT, DELETE
----------------------------------------------------------------------------------------------------
REPLACE INTO percona_test.sentinela (id, ping) VALUES (1, '1dcc1893f940d4b0615d8cf540d52e8c') 


----------------------------------------------------------------------------------------------------
Grants : INSERT, FILE
----------------------------------------------------------------------------------------------------
LOAD DATA INFILE '/tmp/load_data_test.24252' INTO TABLE percona_test.load_data 


----------------------------------------------------------------------------------------------------
Grants : SELECT
----------------------------------------------------------------------------------------------------
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */
USE `sakila`
commit
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */
SET @slave_uuid= 'cd111826-0a1e-11e8-98c3-88787342351a'
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */
SELECT @@GLOBAL.SERVER_ID
/*!50003 SET character_set_client  = @saved_cs_client */
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */
/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */
SET UNIQUE_CHECKS=0
CHECKSUM TABLES mysql.columns_priv, mysql.db, mysql.engine_cost, mysql.event, mysql.func, mysql.gtid ... (truncated)
SET @master_heartbeat_period= 30000001024
SELECT @master_binlog_checksum
rollback
/*!40101 SET @saved_cs_client     = @@character_set_client */
SELECT @@GLOBAL.GTID_MODE
UNLOCK TABLES
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */
SELECT VERSION()
SELECT i FROM percona_test.load_data
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */
/*!50003 SET @saved_cs_results     = @@character_set_results */
SELECT MD5(RAND())
select @@version_comment limit 1
SET AUTOCOMMIT=1
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */
SELECT UNIX_TIMESTAMP()
/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */
/*!40103 SET TIME_ZONE='+00:00' */
SET FOREIGN_KEY_CHECKS=0
/*!40101 SET SQL_MODE=@OLD_SQL_MODE */
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */
SET NAMES utf8
/*!50003 SET @saved_col_connection = @@collation_connection */
/*!50003 SET character_set_results = latin1 */
/*!40101 SET character_set_client = utf8 */
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */
/*!50003 SET character_set_client  = latin1 */
/*!50003 SET collation_connection  = latin1_swedish_ci */
SELECT @@GLOBAL.SERVER_UUID
/*!40101 SET NAMES utf8 */
/*!40101 SET character_set_client = @saved_cs_client */
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */
SELECT DATABASE()
/*!50003 SET @saved_cs_client      = @@character_set_client */
/*!50003 SET @saved_sql_mode       = @@sql_mode */
/*!50003 SET collation_connection  = @saved_col_connection */
SET @master_binlog_checksum= @@global.binlog_checksum
/*!50003 SET sql_mode              = '' */
/*!50003 SET sql_mode              = @saved_sql_mode */
/*!50003 SET character_set_results = @saved_cs_results */
SHOW TABLES FROM mysql
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */ 


----------------------------------------------------------------------------------------------------
Grants : SELECT, CREATE VIEW
----------------------------------------------------------------------------------------------------
CREATE DEFINER=CURRENT_USER SQL SECURITY INVOKER VIEW actor_info
AS
SELECT
a.actor_id,
a.first_name, ... (truncated)
CREATE VIEW sales_by_film_category
AS
SELECT
c.name AS category
, SUM(p.amount) AS total_sales
FROM  ... (truncated)
CREATE VIEW staff_list
AS
SELECT s.staff_id AS ID, CONCAT(s.first_name, _utf8' ', s.last_name) AS na ... (truncated)
CREATE VIEW sales_by_store
AS
SELECT
CONCAT(c.city, _utf8',', cy.country) AS store
, CONCAT(m.first_ ... (truncated)
CREATE VIEW customer_list
AS
SELECT cu.customer_id AS ID, CONCAT(cu.first_name, _utf8' ', cu.last_na ... (truncated)
CREATE VIEW film_list
AS
SELECT film.film_id AS FID, film.title AS title, film.description AS descri ... (truncated)
CREATE VIEW nicer_but_slower_film_list
AS
SELECT film.film_id AS FID, film.title AS title, film.desc ... (truncated) 


----------------------------------------------------------------------------------------------------
Grants : SELECT, INSERT
----------------------------------------------------------------------------------------------------
ANALYZE TABLE actor, address, category, city, country, customer, film, film_actor, film_category, fi ... (truncated) 


----------------------------------------------------------------------------------------------------
Grants : SELECT, LOCK TABLES
----------------------------------------------------------------------------------------------------
LOCK TABLES `language` WRITE
LOCK TABLES `store` WRITE
LOCK TABLES `category` WRITE
LOCK TABLES `city` WRITE
LOCK TABLES `film_actor` WRITE
LOCK TABLES `film_text` WRITE
LOCK TABLES `actor` WRITE
LOCK TABLES `rental` WRITE
LOCK TABLES `payment` WRITE
LOCK TABLES `film_category` WRITE
LOCK TABLES `customer` WRITE
LOCK TABLES `staff` WRITE
LOCK TABLES `film` WRITE
LOCK TABLES `inventory` WRITE
LOCK TABLES `address` WRITE
LOCK TABLES `country` WRITE


----------------------------------------------------------------------------------------------------
Grants : SELECT, PROCESS
----------------------------------------------------------------------------------------------------
SHOW /*!40100 ENGINE*/ INNODB STATUS


----------------------------------------------------------------------------------------------------
Grants : SELECT, RELOAD
----------------------------------------------------------------------------------------------------
FLUSH TABLES


----------------------------------------------------------------------------------------------------
Grants : TRIGGER
----------------------------------------------------------------------------------------------------
/*!50003 CREATE*/ /*!50017 DEFINER=`msandbox`@`%`*/ /*!50003 TRIGGER rental_date BEFORE INSERT ON re ... (truncated)
/*!50003 CREATE*/ /*!50017 DEFINER=`msandbox`@`%`*/ /*!50003 TRIGGER payment_date BEFORE INSERT ON p ... (truncated)
/*!50003 CREATE*/ /*!50017 DEFINER=`msandbox`@`%`*/ /*!50003 TRIGGER customer_create_date BEFORE INS ... (truncated)


----------------------------------------------------------------------------------------------------
Grants : UPDATE
----------------------------------------------------------------------------------------------------
UPDATE mysql.proc SET created='2012-06-05 00:00:00', modified='2012-06-05 00:00:00'

```

# TODO
- [ ] RDS support

## Authors
- **Carlos Salguero** - Initial work

## Acknowledgments
- **Alexander Rubin** - Specs review
- **Roel Van de Paar** - Specs review, sandbox specs, testing
