package models

import (
	"time"
)

// Debtor структура, описывающая должника
type Debtor struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	LoanAmount   float64   `json:"loan_amount"`
	InterestRate float64   `json:"interest_rate"`
	StartDate    time.Time `json:"start_date"`
	Payments     []Payment `json:"payments"`
}

// Payment структура, описывающая платеж
type Payment struct {
	ID        int       `json:"id"`
	DebtorID  int       `json:"debtor_id"`
	Date      time.Time `json:"date"`
	Amount    float64   `json:"amount"`
	IsForLoan bool      `json:"is_for_loan"`
}

// DebtorList структура, содержащая список должников
type DebtorList struct {
	Debtors []Debtor `json:"debtors"`
}

// NewDebtor создает нового должника
func NewDebtor(name string, loanAmount float64, interestRate float64) Debtor {
	return Debtor{
		Name:         name,
		LoanAmount:   loanAmount,
		InterestRate: interestRate,
		StartDate:    time.Now(),
		Payments:     []Payment{},
	}
}

// NewPayment создает новый платеж
func NewPayment(debtorID int, amount float64, isForLoan bool) Payment {
	return Payment{
		DebtorID:  debtorID,
		Date:      time.Now(),
		Amount:    amount,
		IsForLoan: isForLoan,
	}
}
