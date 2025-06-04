package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io" 
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync" // Import sync for mutex
	"time"
)

// Difficulty - Number of leading zeros required for a valid hash
const difficulty = 6

// Transaction represents a single transaction in a block
type Transaction struct {
	ID        string `json:"id"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    int    `json:"amount"`
	Timestamp string `json:"timestamp"`
}

// Block represents a block in the blockchain
type Block struct {
	Index        int           `json:"index"`
	Timestamp    string        `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	Hash         string        `json:"hash"`
	PrevHash     string        `json:"prevHash"`
	Nonce        int           `json:"nonce"`
}

// TransactionInput is used to decode incoming transaction requests from the API
type TransactionInput struct {
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    int    `json:"amount"`
	// Timestamp could optionally be sent, but server-side generation is safer
}

// Function to generate a simple unique ID for a transaction (using hash)
func generateTransactionID(tx Transaction) string {
	record := tx.Sender + tx.Recipient + strconv.Itoa(tx.Amount) + tx.Timestamp
	h := sha256.New()
	h.Write([]byte(record))
	return hex.EncodeToString(h.Sum(nil))
}

// sortTransactions deterministically sorts transactions for consistent hashing
func sortTransactions(txs []Transaction) {
	sort.SliceStable(txs, func(i, j int) bool {
		return txs[i].ID < txs[j].ID
	})
}

// transactionsToString generates a deterministic string representation of transactions for hashing
func transactionsToString(txs []Transaction) string {
	txsCopy := make([]Transaction, len(txs))
	copy(txsCopy, txs)

	sortTransactions(txsCopy)

	var sb strings.Builder
	for _, tx := range txsCopy {
		sb.WriteString(tx.ID)
		sb.WriteString(tx.Sender)
		sb.WriteString(tx.Recipient)
		sb.WriteString(strconv.Itoa(tx.Amount))
	}
	return sb.String()
}

// calculateHash generates the SHA256 hash for a block
func calculateHash(block Block) string {
	transactionsStr := transactionsToString(block.Transactions)
	record := strconv.Itoa(block.Index) + block.Timestamp + transactionsStr + block.PrevHash + strconv.Itoa(block.Nonce)
	h := sha256.New()
	h.Write([]byte(record))
	hashed := h.Sum(nil)
	return hex.EncodeToString(hashed)
}

// isHashValid checks if a hash meets the difficulty criteria
func isHashValid(hash string, difficulty int) bool {
	if len(hash) < difficulty {
		return false
	}
	prefix := strings.Repeat("0", difficulty)
	return strings.HasPrefix(hash, prefix)
}

// isBlockValid checks if a new block is valid
func isBlockValid(newBlock, oldBlock Block, difficulty int) bool {
	if oldBlock.Index+1 != newBlock.Index {
		log.Printf("Validation failed for Block %d: Index mismatch. Expected %d, got %d", newBlock.Index, oldBlock.Index+1, newBlock.Index)
		return false
	}
	if oldBlock.Hash != newBlock.PrevHash {
		log.Printf("Validation failed for Block %d: Previous hash mismatch. Expected %s, got %s", newBlock.Index, oldBlock.Hash, newBlock.PrevHash)
		return false
	}
	// Re-calculate the hash to verify it matches the one in the block header
    // This ensures the Nonce and Transactions were correctly used to find the hash
    if calculateHash(newBlock) != newBlock.Hash {
		log.Printf("Validation failed for Block %d: Hash recalculation mismatch. Recalculated %s, stored %s", newBlock.Index, calculateHash(newBlock), newBlock.Hash)
        return false
    }

	// Only mined blocks are expected to pass the difficulty check in automatic modes
	if selectedMode != 1 && !isHashValid(newBlock.Hash, difficulty) {
		log.Printf("Validation failed for Block %d: Hash %s does not meet difficulty %d ('%s' prefix)", newBlock.Index, newBlock.Hash, difficulty, strings.Repeat("0", difficulty))
		return false
	}

	return true
}

// worker finds a valid nonce within a given range
func worker(ctx context.Context, startNonce, endNonce int, oldBlock Block, transactions []Transaction, difficulty int, resultChan chan<- Block) {
	candidateBlock := Block{
		Index:     oldBlock.Index + 1,
		Timestamp: time.Now().String(), 
		Transactions: transactions,
		PrevHash:  oldBlock.Hash,
		Nonce:     startNonce,
	}

	// Sort transactions ONCE per block candidate for deterministic hashing
	sortTransactions(candidateBlock.Transactions)

	for nonce := startNonce; nonce <= endNonce; nonce++ {
		select {
		case <-ctx.Done():
			return // Stop this goroutine
		default:
		}

		candidateBlock.Nonce = nonce
		candidateHash := calculateHash(candidateBlock)

		if isHashValid(candidateHash, difficulty) {
			candidateBlock.Hash = candidateHash
			select {
			case resultChan <- candidateBlock:
			case <-ctx.Done():
			}
			return
		}
	}
}

// mineBlock orchestrates parallel mining
func mineBlock(oldBlock Block, transactions []Transaction, difficulty int, numWorkers int) Block {
	fmt.Printf("Mining block %d with %d worker(s) (%d transactions)...\n", oldBlock.Index+1, numWorkers, len(transactions))

	const maxNonceSearch = 2000000000

	resultChan := make(chan Block)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure cancel is called when mineBlock exits

	chunkSize := int(math.Ceil(float64(maxNonceSearch) / float64(numWorkers)))

	for i := 0; i < numWorkers; i++ {
		startNonce := i * chunkSize
		endNonce := startNonce + chunkSize - 1
		if endNonce >= maxNonceSearch {
            endNonce = maxNonceSearch - 1
            if startNonce >= maxNonceSearch {
                 break
            }
        }
        if startNonce < 0 { startNonce = 0 }

		// Pass a COPY of the transactions slice to each worker!
		// This prevents workers modifying the same slice header or underlying array
		txsCopy := make([]Transaction, len(transactions))
        copy(txsCopy, transactions)
		go worker(ctx, startNonce, endNonce, oldBlock, txsCopy, difficulty, resultChan)
	}

	// Add a timeout for mining in case difficulty is too high for maxNonceSearch
	// Or simply wait for a result indefinitely until context is cancelled externally (e.g. new block found by another node - not implemented here)
	// For this visualization, waiting for the result channel is fine, as we are the only miner.

	// Wait for the first result
	minedBlock := <-resultChan

	// The defer cancel() will now run, stopping other workers

	fmt.Printf("Block %d mined with hash %s (Nonce: %d)\n", minedBlock.Index, minedBlock.Hash, minedBlock.Nonce)

	// Give workers a moment to receive the cancel signal
	// time.Sleep(10 * time.Millisecond) // Often not strictly necessary with defer, but harmless

	return minedBlock
}


var Blockchain []Block // Global blockchain
var selectedMode int
var totalBlocksToProcess int
var miningMode string

// --- Transaction Pool ---
var pendingTransactions []Transaction // Slice to hold transactions received but not yet mined
var txMutex sync.Mutex // Mutex to protect access to pendingTransactions


// exportBlockchainToJSON saves the current blockchain to a JSON file
func exportBlockchainToJSON() {
	data, err := json.MarshalIndent(Blockchain, "", "  ")
	if err != nil {
		log.Printf("Error marshaling blockchain: %v", err)
		return
	}
	err = os.WriteFile("blockchain.json", data, 0644) // Use os.WriteFile
	if err != nil {
		log.Printf("Error writing blockchain.json: %v", err)
		return
	}
	// fmt.Println("Blockchain data exported to blockchain.json") // Avoid spamming during mining
}

// openBrowser opens the specified URL
func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	err := exec.Command(cmd, args...).Start()
	if err != nil {
		log.Printf("Failed to open browser: %v. Please open %s manually.", err, url)
	}
}

// generateRandomTransactions creates a slice of random transactions (kept for potential future use or other modes)
func generateRandomTransactions(count int) []Transaction {
    txs := make([]Transaction, count)
    senders := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Miner"}
    recipients := []string{"Alice", "Bob", "Charlie", "David", "Eve", "Miner"}

    for i := 0; i < count; i++ {
        sender := senders[rand.Intn(len(senders))]
        recipient := recipients[rand.Intn(len(recipients))]
        for sender == recipient && len(senders) > 1 {
             recipient = recipients[rand.Intn(len(recipients))]
        }

        amount := rand.Intn(100) + 1

        tx := Transaction{
            Sender: sender,
            Recipient: recipient,
            Amount: amount,
            Timestamp: time.Now().Format(time.RFC3339Nano),
        }
        tx.ID = generateTransactionID(tx)
        txs[i] = tx
    }
    return txs
}

// --- HTTP Handlers ---

func setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS") // Added POST
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	// Handle OPTIONS preflight request
	if r.Method == "OPTIONS" {
		return
	}
}

// rootHandler serves index.html or index2.html
func rootHandler(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)
    if r.Method == "OPTIONS" { return } // Handle preflight

	if selectedMode == 3 {
		log.Println("Serving index2.html for Mode 3")
		http.ServeFile(w, r, "index2.html")
	} else {
		log.Println("Serving index.html for Mode", selectedMode)
		http.ServeFile(w, r, "index.html")
	}
}

// blockchainHandler serves the current state of the blockchain
func blockchainHandler(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w, r)
    if r.Method == "OPTIONS" { return } // Handle preflight

	w.Header().Set("Content-Type", "application/json")

	// No need for mutex here, reading from Blockchain is safe assuming appends are atomic enough or done sequentially in the mining goroutine.
    // If Blockchain could be replaced entirely, a read lock would be needed. For simple appends, it's generally fine.

	response := struct {
		Blockchain           []Block `json:"blockchain"`
		TotalBlocksToProcess int     `json:"totalBlocksToProcess"`
        PendingTransactionsCount int `json:"pendingTransactionsCount"` // Add pending count
	}{
		Blockchain:           Blockchain,
		TotalBlocksToProcess: totalBlocksToProcess,
        PendingTransactionsCount: len(pendingTransactions), // Include pending count
	}

	json.NewEncoder(w).Encode(response)
}

// transactionHandler receives new transactions via POST requests
func transactionHandler(w http.ResponseWriter, r *http.Request) {
    setCORSHeaders(w, r)
    if r.Method == "OPTIONS" { // Handle preflight
        w.WriteHeader(http.StatusOK)
        return
    }

    if r.Method != "POST" {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    body, err := io.ReadAll(r.Body) // Use io.ReadAll
    if err != nil {
        http.Error(w, "Error reading request body", http.StatusInternalServerError)
        log.Printf("Error reading request body: %v", err)
        return
    }

    var txInput TransactionInput
    err = json.Unmarshal(body, &txInput)
    if err != nil {
        http.Error(w, "Error decoding request body: Invalid JSON", http.StatusBadRequest)
        log.Printf("Error decoding request body: %v", err)
        return
    }

    // Basic validation
    if txInput.Sender == "" || txInput.Recipient == "" || txInput.Amount <= 0 {
        http.Error(w, "Invalid transaction data: Sender, Recipient, and Amount (must be > 0) are required", http.StatusBadRequest)
        log.Printf("Invalid transaction data received: %+v", txInput)
        return
    }

    // Create the full Transaction object (server generates timestamp and ID)
    newTx := Transaction{
        Sender:    txInput.Sender,
        Recipient: txInput.Recipient,
        Amount:    txInput.Amount,
        Timestamp: time.Now().Format(time.RFC3339Nano), // Server timestamp
    }
    newTx.ID = generateTransactionID(newTx) // Server generates ID

    // Add the transaction to the pending pool safely
    txMutex.Lock()
    pendingTransactions = append(pendingTransactions, newTx)
    txMutex.Unlock()

    log.Printf("Received new transaction: %+v. %d pending transactions.", newTx, len(pendingTransactions))

    w.WriteHeader(http.StatusCreated)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"message": "Transaction received and added to pool", "transactionID": newTx.ID})

    // Introduce a small delay after releasing the mutex
    time.Sleep(1 * time.Millisecond)
}


func main() {
	rand.Seed(time.Now().UnixNano())

	// Create the genesis block
	genesisBlock := Block{Index: 0, Timestamp: time.Now().String(), Transactions: []Transaction{}, Hash: "", PrevHash: "", Nonce: 0}
	genesisBlock.Hash = calculateHash(genesisBlock)
	Blockchain = append(Blockchain, genesisBlock)
	fmt.Println("Genesis block created.")
	fmt.Printf("Genesis Hash: %s (Nonce: %d)\n", genesisBlock.Hash, genesisBlock.Nonce)

	// --- Mode Selection ---
	fmt.Println("\nChoose a mode:")
	fmt.Println("1. Manual Block Entry (Blocks added directly via console, dummy transaction, no PoW mining)")
	fmt.Println("2. Automatic Block Generation & Mining (Single dummy transaction)")
	fmt.Println("3. Automatic Block Generation & Mining (Including Transactions from API Pool)") // Updated Mode 3 description
	fmt.Print("Enter mode number (1, 2, or 3): ")

	var inputMode int
	_, err := fmt.Scanln(&inputMode)
	if err != nil || (inputMode != 1 && inputMode != 2 && inputMode != 3) {
		log.Fatalf("Invalid input for mode selection. Please enter 1, 2, or 3.")
		os.Exit(1)
	}
	selectedMode = inputMode

	// Get number of blocks to process if in automatic modes (2 or 3)
	if selectedMode == 2 || selectedMode == 3 {
		fmt.Print("Enter Number of Blocks to Process (mine): ")
		var inputNumBlocks int
		_, err = fmt.Scanln(&inputNumBlocks)
		if err != nil {
			log.Printf("Error reading number of blocks input: %v", err)
			os.Exit(1)
		}
		if inputNumBlocks <= 0 {
			log.Fatalf("Number of blocks must be positive.")
			os.Exit(1)
		}
		totalBlocksToProcess = inputNumBlocks

		fmt.Println("\nChoose automatic mining mode:")
		fmt.Println("a. Single-threaded Mining (1 worker)")
		fmt.Println("b. Multi-threaded Mining (using CPU cores)")
		fmt.Print("Enter mining mode (a or b): ")
		_, err = fmt.Scanln(&miningMode)
		miningMode = strings.ToLower(miningMode)
		if err != nil || (miningMode != "a" && miningMode != "b") {
			log.Fatalf("Invalid input for mining mode. Please enter 'a' or 'b'.")
			os.Exit(1)
		}
	}

	// --- HTTP server setup ---
	// Register the root handler (serves index.html or index2.html)
	http.HandleFunc("/", rootHandler)
	// Register the blockchain data handler
	http.HandleFunc("/blockchain", blockchainHandler)
    // Register the new transaction handler
    http.HandleFunc("/transaction", transactionHandler)


	// Start the server in a goroutine
	go func() {
		fmt.Println("\nStarting server on :8081. Keep this window open.")
		fmt.Println("View the visualization at http://localhost:8081/")
        if selectedMode == 3 {
             fmt.Println("Submit transactions via POST to http://localhost:8081/transaction")
        }
		err := http.ListenAndServe(":8081", nil)
		if err != nil {
			log.Fatalf("Server error: %v", err)
		}
		fmt.Println("Server stopped.")
	}()


	// --- Block Generation Logic based on Mode ---
	// Run generation/mining in a separate goroutine
	go func() {
		switch selectedMode {
		case 1:
			fmt.Println("\n--- Manual Block Entry Mode Selected ---")
			// Manual mode logic remains similar, but now uses the transaction structure
			var money int
			fmt.Print("Enter Amount value for dummy transaction (-1 to stop adding blocks): ")
			_, err := fmt.Scanln(&money)
			if err != nil {
				log.Printf("Error reading Amount input: %v", err)
			}

			for money != -1 {
				lastBlock := Blockchain[len(Blockchain)-1]
				transactions := []Transaction{}
				if money >= 0 {
					tx := Transaction{
						Sender:    "Manual",
						Recipient: "BlockData",
						Amount:    money,
						Timestamp: time.Now().Format(time.RFC3339Nano),
					}
					tx.ID = generateTransactionID(tx)
					transactions = append(transactions, tx)
				}

				newBlock := Block{
					Index:        lastBlock.Index + 1,
					Timestamp:    time.Now().String(),
					Transactions: transactions,
					PrevHash:     lastBlock.Hash,
					Nonce:        0,
				}
				newBlock.Hash = calculateHash(newBlock)

				// In manual mode, we add blocks even if they don't meet PoW difficulty (Nonce 0)
                // We check isBlockValid primarily for structural integrity (index, prevHash, self-hash calculation)
                isValid := isBlockValid(newBlock, lastBlock, difficulty) // Pass difficulty, but expect failure for Nonce=0

                if isValid && isHashValid(newBlock.Hash, difficulty) {
                     // This block surprisingly passed difficulty (highly unlikely for Nonce=0)
                     fmt.Printf("Manual block %d generated and PASSED difficulty %d! Added.\n", newBlock.Index, difficulty)
                } else if isValid && !isHashValid(newBlock.Hash, difficulty) {
                    // This is the expected case for a valid manual block with Nonce=0 and difficulty > 0
                    fmt.Printf("Manual block %d generated but does NOT meet mining difficulty %d. Added anyway.\n", newBlock.Index, difficulty)
                } else {
                     // isBlockValid failed for other reasons (index, prevHash, or calculation mismatch)
                     log.Printf("Manual block %d validation failed structurally. Not added.", newBlock.Index)
                     fmt.Printf("Manual block %d validation failed structurally. Not added.\n", newBlock.Index)
                     goto nextManualInput // Skip adding and go to the next input prompt
                }

				Blockchain = append(Blockchain, newBlock)
				fmt.Printf("Added block %d (Manual) with %d transactions (Nonce: %d, Valid PoW: %t)\n", newBlock.Index, len(newBlock.Transactions), newBlock.Nonce, isHashValid(newBlock.Hash, difficulty))

			nextManualInput:
				fmt.Print("Enter Amount value for dummy transaction (-1 to stop adding blocks): ")
				_, err = fmt.Scanln(&money)
				if err != nil {
					log.Printf("Error reading Amount input: %v", err)
					break
				}
			}
			fmt.Println("\nManual entry finished.")
			exportBlockchainToJSON()


		case 2:
            fmt.Println("\n--- Automatic Block Generation & Mining Mode Selected (Single Dummy Transaction) ---")
            fmt.Printf("Mining %d blocks with difficulty %d ('%s' prefix)...\n", totalBlocksToProcess, difficulty, strings.Repeat("0", difficulty))

            numWorkers := 1
            if miningMode == "b" {
                numWorkers = runtime.NumCPU()
                if numWorkers < 1 { numWorkers = 1 }
            }
            fmt.Printf("Using %d worker(s).\n", numWorkers)

            for i := 0; i < totalBlocksToProcess; i++ {
                lastBlock := Blockchain[len(Blockchain)-1]

                 // Create a single dummy transaction for this block
                 randomAmount := rand.Intn(500000) + 1
                 dummyTx := Transaction{
                      Sender: "System",
                      Recipient: "Reward",
                      Amount: randomAmount,
                      Timestamp: time.Now().Format(time.RFC3339Nano),
                 }
                 dummyTx.ID = generateTransactionID(dummyTx)

                 transactionsForThisBlock := []Transaction{dummyTx}

                newBlock := mineBlock(lastBlock, transactionsForThisBlock, difficulty, numWorkers)

                if isBlockValid(newBlock, lastBlock, difficulty) {
                    Blockchain = append(Blockchain, newBlock)
                } else {
                    log.Println("Error: Automatic mining (Mode 2) created an invalid block!")
                    break
                }
                 // Small pause between mining blocks
                 time.Sleep(50 * time.Millisecond)
            }
            fmt.Println("Automatic mining complete (Mode 2).")
            exportBlockchainToJSON()


		case 3:
			fmt.Println("\n--- Automatic Block Generation & Mining Mode Selected (Including Transactions from API Pool) ---")
			fmt.Printf("Mining %d blocks with difficulty %d ('%s' prefix) using transactions from the API pool...\n", totalBlocksToProcess, difficulty, strings.Repeat("0", difficulty))

			numWorkers := 1
			if miningMode == "b" {
				numWorkers = runtime.NumCPU()
				if numWorkers < 1 {
					numWorkers = 1
				}
			}
			fmt.Printf("Using %d worker(s).\n", numWorkers)

			// Mining loop for Mode 3
			for i := 0; i < totalBlocksToProcess; i++ {
				time.Sleep(500 * time.Millisecond)
				lastBlock := Blockchain[len(Blockchain)-1]

				// --- Collect transactions from the pending pool ---
				txMutex.Lock()
				transactionsToInclude := make([]Transaction, len(pendingTransactions))
				copy(transactionsToInclude, pendingTransactions)
				pendingTransactions = []Transaction{} // Clear the pool AFTER copying
				txMutex.Unlock()

                // If no transactions are pending, the block will be mined with 0 transactions
				if len(transactionsToInclude) == 0 {
                    fmt.Printf("No pending transactions for block %d. Mining empty block...\n", lastBlock.Index + 1)
                } else {
                    fmt.Printf("Including %d transactions in block %d...\n", len(transactionsToInclude), lastBlock.Index + 1)
                }


				// --- Mine the block with collected transactions ---
				newBlock := mineBlock(lastBlock, transactionsToInclude, difficulty, numWorkers)

				// Validate the mined block
				if isBlockValid(newBlock, lastBlock, difficulty) {
					Blockchain = append(Blockchain, newBlock)
                    fmt.Printf("Successfully added block %d with %d transactions.\n", newBlock.Index, len(newBlock.Transactions))
					time.Sleep(100 * time.Millisecond) // Was 50ms, adjusted slightly
				} else {
					log.Println("Error: Automatic mining (Mode 3) created an invalid block!")
					// Optional: Decide how to handle this - retry mining? log and skip?
					break // Stop mining on error for simplicity
				}

				// Small pause after adding a block to give time for new transactions to arrive via API
				time.Sleep(50 * time.Millisecond) // Adjust if needed
			}
			fmt.Println("Automatic mining complete (Mode 3).")
			exportBlockchainToJSON()


		default:
			return // Should not happen
		}
	}() // End of mining goroutine

	// Give the server a moment to start before opening the browser
	time.Sleep(500 * time.Millisecond)

	// Open the browser
	url := "http://localhost:8081/"
	fmt.Printf("Opening browser to %s...\n", url)
	openBrowser(url)

	// Prevent main from exiting
	select {}

}