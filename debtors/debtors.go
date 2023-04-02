package debtors

import (
	"fmt"
	"time"

	"github.com/jtprogru/loans/models"
)

// CalculateInterest рассчитывает начисленные проценты
func CalculateInterest(debtor *models.Debtor) float64 {
	duration := time.Since(debtor.StartDate)
	weeks := int(duration.Hours() / (24 * 7 * 3))

	interest := debtor.LoanAmount

	for i := 0; i < weeks; i++ {
		interest *= (1 + debtor.InterestRate)
	}

	return interest - debtor.LoanAmount
}

// PrintDebtorsWithoutInterest выводит список должников без учета начисленных процентов
func PrintDebtorsWithoutInterest(debtorList *models.DebtorList) {
	fmt.Println("Debtors without interest:")
	for _, debtor := range debtorList.Debtors {
		fmt.Printf("ID: %d, Name: %s, Loan amount: %.2f\n", debtor.ID, debtor.Name, debtor.LoanAmount)
	}
}

// PrintDebtorsWithInterest выводит список должников с учетом начисленных процентов
func PrintDebtorsWithInterest(debtorList *models.DebtorList) {
	fmt.Println("Debtors with interest:")
	for _, debtor := range debtorList.Debtors {
		interest := CalculateInterest(&debtor)
		fmt.Printf("ID: %d, Name: %s, Loan amount: %.2f, Interest: %.2f, Total: %.2f\n", debtor.ID, debtor.Name, debtor.LoanAmount, interest, debtor.LoanAmount+interest)
	}
}

// PrintPaymentsForDebtor выводит список платежей для конкретного должника
func PrintPaymentsForDebtor(debtorList *models.DebtorList, debtorID int) {
	for _, debtor := range debtorList.Debtors {
		if debtor.ID == debtorID {
			fmt.Printf("Debtor ID: %d, Name: %s\n", debtor.ID, debtor.Name)
			for _, payment := range debtor.Payments {
				paymentType := "Loan"
				if !payment.IsForLoan {
					paymentType = "Interest"
				}
				fmt.Printf("  Payment ID: %d, Date: %s, Amount: %.2f, Type: %s\n", payment.ID, payment.Date.Format("2006-01-02"), payment.Amount, paymentType)
			}
			return
		}
	}
	fmt.Printf("Debtor with ID %d not found.\n", debtorID)
}
