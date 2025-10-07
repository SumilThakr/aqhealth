# Attribution Methods

This document explains the two attribution methods available in aqhealth for calculating mortality attributable to PM2.5 sources.

## Overview

Attribution methods determine how mortality is assigned to a specific PM2.5 source when multiple sources contribute to total PM2.5 concentrations.

## Methods

### 1. Proportional Attribution (Default)

**Configuration:** `"attributionMethod": "proportional"`

**Formula:**
```
attributed_deaths = (resultpm / totpm) × total_deaths
```

**Interpretation:**
- Assigns deaths proportionally based on the source's fractional contribution to total PM2.5
- Answers: "What fraction of deaths are associated with this source?"
- Uses maximum concentration approach: `conc = max(resultpm, totpm)`

**Use Cases:**
- Relative source apportionment
- Multi-source attribution where sources sum to total
- Standard epidemiological attribution studies

**Example:**
- Total PM2.5 = 20 μg/m³
- Source contribution = 5 μg/m³
- Total deaths = 1000
- Attributed deaths = (5/20) × 1000 = 250 deaths

### 2. Zero-Out Attribution

**Configuration:** `"attributionMethod": "zeroout"`

**Formula:**
```
attributed_deaths = deaths(totpm + resultpm) - deaths(totpm)
```

**Interpretation:**
- Calculates deaths that would be avoided if the source were completely removed
- Answers: "How many deaths would be prevented if we eliminated this source?"
- Uses sum of concentrations: `conc = totpm + resultpm`
- Includes robust NaN and division-by-zero handling

**Use Cases:**
- Policy scenario analysis (source elimination)
- Intervention impact assessment
- Counterfactual analysis ("what if" scenarios)
- Cases with missing or invalid data (NaN handling)

**Example:**
- Baseline PM2.5 = 20 μg/m³ → 1000 deaths
- With source PM2.5 = 25 μg/m³ → 1200 deaths
- Attributed deaths = 1200 - 1000 = 200 deaths

## Key Differences

| Aspect | Proportional | Zero-Out |
|--------|-------------|----------|
| **Concentration** | max(resultpm, totpm) | totpm + resultpm |
| **Formula** | (resultpm/totpm) × deaths | deaths(total) - deaths(baseline) |
| **Meaning** | Fraction of deaths from source | Deaths avoided if source removed |
| **Sum property** | Multiple sources can sum to 100% | Sources may not sum to 100% |
| **NaN handling** | Basic | Extensive |
| **Non-linearity** | Linear apportionment | Accounts for non-linear dose-response |

## Technical Implementation

### Proportional Method Functions
- `totDeaths()` - Uses max concentration
- `attribution()` - Proportional formula

### Zero-Out Method Functions
- `totDeathsSum()` - Sums concentrations with NaN handling
- `baseDeaths()` - Baseline scenario (no source)
- `zeroOut()` - Difference calculation

## Configuration Examples

### Proportional Attribution
```json
{
  "attributionMethod": "proportional",
  "outputSpec": {
    "mode": "allcause"
  }
}
```

### Zero-Out Attribution
```json
{
  "attributionMethod": "zeroout",
  "outputSpec": {
    "mode": "5cod"
  }
}
```

### Command-Line Override
```bash
./aqhealth --config config.json --attributionMethod zeroout
```

## Choosing a Method

**Use Proportional when:**
- You need relative source contributions
- Multiple sources should sum to total
- Standard epidemiological attribution is required
- Data is complete and well-behaved

**Use Zero-Out when:**
- Evaluating policy interventions (source removal)
- Performing counterfactual analysis
- Data contains NaN or invalid values
- Accounting for non-linear concentration-response relationships is important
- Answering "what if we eliminate this source?" questions

## References

- Proportional attribution is the traditional approach in air quality health impact assessment
- Zero-out methodology is increasingly used for policy scenario analysis
- Both methods are compatible with all output modes (allcause, 5cod, individual, multiple)
