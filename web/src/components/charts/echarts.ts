import { LineChart } from 'echarts/charts'
import {
  DataZoomComponent,
  GridComponent,
  LegendComponent,
  MarkLineComponent,
  MarkPointComponent,
  TooltipComponent,
} from 'echarts/components'
import * as echarts from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'

// Only the parts the option builders use are registered, so the bundle does
// not carry every chart type ECharts ships. A new series type or component in
// an option builder must be added here too, or ECharts ignores it.
echarts.use([
  CanvasRenderer,
  DataZoomComponent,
  GridComponent,
  LegendComponent,
  LineChart,
  MarkLineComponent,
  MarkPointComponent,
  TooltipComponent,
])

export { echarts }
