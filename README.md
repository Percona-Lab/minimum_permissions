# minimum_permissions
Get the minimum set of permissions needed to run a particular query

## Example
Suppose you have this database and table:
```
drop database if exists issue_907;
create database issue_907;
create table issue_907.t (i int);
insert into issue_907.t values (1);
```
  
and you need to run this query:

```
SELECT `i`, COUNT(*) AS test_count FROM `issue_907`.`t` WHERE 1=1 GROUP BY i ORDER BY i LOCK IN SHARE MODE
```
  
which are the minimum permissions a user need to be able to run the query?

This program try all permissions getting the minimum set needed to run the query.

## Usage
1. Put the queries needed to prepare the environment in a file:
```cat prepare.sql```
```
drop database if exists issue_907;
create database issue_907;
create table issue_907.t (i int);
insert into issue_907.t values (1);
```
2. Run the program with a specifying a user with SUPER and GRANT OPTION:
```
minimum_permissions --user=root --password=msandbox --host=127.0.0.1 --port=3306 --prepare-file=prepare.sql --test-statement='SELECT i, COUNT(*) AS test_count FROM issue_907.t WHERE 1=1 GROUP BY i ORDER BY i LOCK IN SHARE MODE'
```

The output will be something like this:
```
Minimum working permissions:
GRANT  UPDATE, SELECT ON *.* TO `testuser`@`127.0.0.1` IDENTIFIED BY 'testpwd'
```
