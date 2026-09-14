import { colors, functionalColors, neutralColors } from './color'
import { fontSize, fontFamily, lineHeight } from './typography'
import { spacing, componentSpacing } from './spacing'
import { borderRadius, boxShadow } from './effects'
import { animation } from './animation'

export const theme = {
  colors: {
    ...colors,
    ...functionalColors,
    ...neutralColors,
  },
  typography: {
    fontSize,
    fontFamily,
    lineHeight,
  },
  spacing: {
    ...spacing,
    ...componentSpacing,
  },
  effects: {
    borderRadius,
    boxShadow,
    animation,
  },
}

export default theme
