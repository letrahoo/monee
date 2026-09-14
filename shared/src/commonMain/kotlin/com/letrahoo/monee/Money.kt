package com.letrahoo.monee

/** Formats integer minor units without floating point conversion, including Long.MIN_VALUE. */
fun formatMoney(minor: Long): String {
    val digits = minor.toString().removePrefix("-").padStart(3, '0')
    val integer = digits.dropLast(2).reversed().chunked(3).joinToString(",").reversed()
    return "${if (minor < 0) "−" else ""}¥$integer.${digits.takeLast(2)}"
}

/** Transaction amounts already carry their expense/income sign. */
fun formatTransactionMoney(minor: Long): String =
    (if (minor > 0) "+" else "") + formatMoney(minor)
