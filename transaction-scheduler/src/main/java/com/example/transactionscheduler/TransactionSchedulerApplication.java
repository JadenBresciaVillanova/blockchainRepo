package com.example.transactionscheduler;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.scheduling.annotation.EnableScheduling;
import org.springframework.context.annotation.ComponentScan; // Import ComponentScan

@SpringBootApplication
@EnableScheduling
@ComponentScan(basePackages = "com.example.transactionscheduler") // Explicitly scan this package
public class TransactionSchedulerApplication {

    public static void main(String[] args) {
        SpringApplication.run(TransactionSchedulerApplication.class, args);
    }
}