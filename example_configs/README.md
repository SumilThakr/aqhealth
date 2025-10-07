# Output Mode Configuration Examples

This directory contains example configuration files demonstrating the different output modes for mortality estimation.

## Output Modes

### 1. All-Cause Mortality (`allcause`)
**File:** `allcause.json`

Calculates all-cause mortality for adults 25+. This is the default mode.

**Output:** Single shapefile specified by `outputFile`

```bash
./aqhealth --config example_configs/allcause.json
```

### 2. Five Causes of Death (`5cod`)
**File:** `5cod.json`

Calculates mortality for 5 major causes of death (COPD, lung cancer, lower respiratory infection, ischemic heart disease, stroke) summed across all age groups.

**Output:** Single shapefile with aggregated mortality from all 5 causes

```bash
./aqhealth --config example_configs/5cod.json
```

### 3. Individual Cause/Age (`individual`)
**File:** `individual_lcancer.json`

Calculates mortality for a specific cause and age group.

**Requirements:**
- Exactly one cause in `causes` array
- Exactly one age in `ages` array

**Output:** Single shapefile specified by `outputFile`

```bash
./aqhealth --config example_configs/individual_lcancer.json
```

### 4. Multiple Cause/Age Combinations (`multiple`)
**File:** `multiple_3causes.json`

Generates separate output files for each cause/age combination.

**Requirements:**
- At least one cause in `causes` array
- At least one age in `ages` array

**Output:** Multiple shapefiles named `{cause}_{age}.shp` in `outputDir`

```bash
./aqhealth --config example_configs/multiple_3causes.json
```

## Available Causes

- `all` - All-cause mortality (only available for age 25)
- `copd` - Chronic obstructive pulmonary disease (age 25)
- `lcancer` - Lung cancer (age 25)
- `lri` - Lower respiratory infection (age 25)
- `ihd` - Ischemic heart disease (ages 27.5-85)
- `str` - Stroke (ages 27.5-85)

## Available Age Groups

- `25` - All adults 25+ (for all, copd, lcancer, lri)
- `27.5`, `32.5`, `37.5`, `42.5`, `47.5`, `52.5`, `57.5`, `62.5`, `67.5`, `72.5`, `77.5`, `85` - Age-specific strata (for ihd, str)

## Example Custom Configurations

### IHD for elderly (85+)
```json
{
  "outputSpec": {
    "mode": "individual",
    "causes": ["ihd"],
    "ages": ["85"]
  }
}
```

### All age strata for IHD
```json
{
  "outputSpec": {
    "mode": "multiple",
    "causes": ["ihd"],
    "ages": ["27.5", "32.5", "37.5", "42.5", "47.5", "52.5", "57.5", "62.5", "67.5", "72.5", "77.5", "85"]
  }
}
```

### Respiratory diseases (COPD + LRI) for adults
```json
{
  "outputSpec": {
    "mode": "multiple",
    "causes": ["copd", "lri"],
    "ages": ["25"]
  }
}
```
