// src/main/java/com/example/transactionscheduler/TransactionPayload.java
package com.example.transactionscheduler;

// This class represents the structure of the JSON body we send to the Go server's /transaction endpoint.
public class TransactionPayload {

    private String sender;
    private String recipient;
    private int amount;

    // Default constructor (often required by JSON libraries)
    public TransactionPayload() {
    }

    // Constructor to easily create a payload object
    public TransactionPayload(String sender, String recipient, int amount) {
        this.sender = sender;
        this.recipient = recipient;
        this.amount = amount;
    }

    // Getters and setters (Jackson uses these by default for serialization/deserialization)
    public String getSender() {
        return sender;
    }

    public void setSender(String sender) {
        this.sender = sender;
    }

    public String getRecipient() {
        return recipient;
    }

    public void setRecipient(String recipient) {
        this.recipient = recipient;
    }

    public int getAmount() {
        return amount;
    }

    public void setAmount(int amount) {
        this.amount = amount;
    }

    @Override
    public String toString() {
        return "TransactionPayload{" +
               "sender='" + sender + '\'' +
               ", recipient='" + recipient + '\'' +
               ", amount=" + amount +
               '}';
    }
}