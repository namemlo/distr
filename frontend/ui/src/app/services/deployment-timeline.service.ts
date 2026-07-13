import {HttpClient, HttpParams} from '@angular/common/http';
import {inject, Injectable} from '@angular/core';
import {Observable} from 'rxjs';
import {
  DeploymentTask,
  DeploymentTaskTimeline,
  DeploymentTimeline,
  DeploymentTimelineCompareRef,
  DeploymentTimelineComparison,
  DeploymentTimelineQuery,
  DeploymentTimelineRedeploy,
} from '../types/deployment-timeline';

const baseUrl = '/api/v1/deployment-timeline';

@Injectable({
  providedIn: 'root',
})
export class DeploymentTimelineService {
  private readonly httpClient = inject(HttpClient);

  list(query: DeploymentTimelineQuery = {}): Observable<DeploymentTimeline> {
    return this.httpClient.get<DeploymentTimeline>(baseUrl, {params: this.queryParams(query)});
  }

  compare(
    base: DeploymentTimelineCompareRef | string,
    compare: DeploymentTimelineCompareRef | string
  ): Observable<DeploymentTimelineComparison> {
    return this.httpClient.get<DeploymentTimelineComparison>(`${baseUrl}/compare`, {
      params: this.compareParams(base, compare),
    });
  }

  redeploy(taskId: string): Observable<DeploymentTimelineRedeploy> {
    return this.httpClient.post<DeploymentTimelineRedeploy>(`${baseUrl}/${taskId}/redeploy`, null);
  }

  getTask(taskId: string): Observable<DeploymentTask> {
    return this.httpClient.get<DeploymentTask>(`/api/v1/tasks/${taskId}`);
  }

  getTaskTimeline(taskId: string): Observable<DeploymentTaskTimeline> {
    return this.httpClient.get<DeploymentTaskTimeline>(`/api/v1/tasks/${taskId}/timeline`);
  }

  private compareParams(
    base: DeploymentTimelineCompareRef | string,
    compare: DeploymentTimelineCompareRef | string
  ): HttpParams {
    let params = new HttpParams();
    params = this.addCompareRefParams(params, 'base', this.normalizeCompareRef(base));
    return this.addCompareRefParams(params, 'compare', this.normalizeCompareRef(compare));
  }

  private normalizeCompareRef(ref: DeploymentTimelineCompareRef | string): DeploymentTimelineCompareRef {
    return typeof ref === 'string' ? {taskId: ref} : ref;
  }

  private addCompareRefParams(
    params: HttpParams,
    prefix: 'base' | 'compare',
    ref: DeploymentTimelineCompareRef
  ): HttpParams {
    if (ref.taskId) {
      return params.set(`${prefix}TaskId`, ref.taskId);
    }
    if (ref.legacyDeploymentRevisionId) {
      return params.set(`${prefix}LegacyDeploymentRevisionId`, ref.legacyDeploymentRevisionId);
    }
    return params;
  }

  private queryParams(query: DeploymentTimelineQuery): HttpParams {
    let params = new HttpParams();
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined && value !== null && value !== '') {
        params = params.set(key, String(value));
      }
    }
    return params;
  }
}
